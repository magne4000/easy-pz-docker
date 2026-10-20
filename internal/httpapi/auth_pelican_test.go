package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/app"
)

func pelicanConfig() app.Config {
	cfg := testConfig()
	cfg.Host, cfg.AdminHash = app.HostPelican, ""
	cfg.PelicanServer, cfg.PelicanPublicKey, cfg.PelicanPanelURL = testServer, testKey, "https://panel.example.com/"
	return cfg
}

func token(t *testing.T, scopes ...string) string {
	return mint(t, testPriv, tokenClaims(time.Now(), scopes...))
}

// operations lists every documented operation.
func operations() []*huma.Operation {
	_, _, api := NewAPI("test", fiber.Config{})
	RegisterRoutes(api, Deps{})
	RegisterAuthRoutes(api)
	var out []*huma.Operation
	for _, item := range api.OpenAPI().Paths {
		for _, op := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op != nil {
				out = append(out, op)
			}
		}
	}
	return out
}

// The permissions contract's table, per operation: a new operation fails here
// until it is given its scope.
func TestEveryOperationHasItsScope(t *testing.T) {
	const public = "public"
	want := map[string]string{
		"get-session": public, "login": public, "login-pelican": public, "logout": public, "create-ticket": scopeRead,

		"get-server-status": scopeRead, "get-console-history": scopeRead, "stream-events": scopeRead, "get-system": scopeRead,
		"get-settings": scopeRead, "list-tasks": scopeRead, "list-mods": scopeRead, "get-mod-conflicts": scopeRead,
		"list-backups": scopeRead, "get-backup": scopeRead, "list-schedules": scopeRead, "get-updates": scopeRead,
		"get-ini": scopeRead, "get-sandbox": scopeRead, "get-paths": scopeRead,

		"start-server": scopeControl, "stop-server": scopeControl, "restart-server": scopeControl,
		"cancel-operation": scopeControl, "save-world": scopeControl, "broadcast": scopeControl,
		"check-updates": scopeControl, "apply-update": scopeControl, "cancel-update": scopeControl,

		"rcon-exec": scopeConsole,

		"add-mods": scopeMods, "remove-mod": scopeMods, "remove-local-mod": scopeMods, "toggle-mod": scopeMods,
		"set-mod-order": scopeMods, "autosort-mods": scopeMods, "check-mod-updates": scopeMods,
		"import-collection": scopeMods, "apply-mods": scopeMods, "upload-local-mod": scopeMods,

		"update-ini": scopeConfig, "update-sandbox": scopeConfig,

		"create-backup": scopeBackups, "pin-backup": scopeBackups, "delete-backup": scopeBackups,
		"restore-backup": scopeBackups, "download-backup": scopeBackups,

		"create-schedule": scopeSchedules, "update-schedule": scopeSchedules, "delete-schedule": scopeSchedules,
		"run-schedule": scopeSchedules,

		"update-settings": scopeSettings,
	}
	got := map[string]string{}
	for _, op := range operations() {
		if op.Metadata[publicMeta] == true {
			got[op.OperationID] = public
			continue
		}
		s, ok := requiredScope(op)
		require.True(t, ok, "%s has no scope", op.OperationID)
		got[op.OperationID] = s
	}
	require.Equal(t, want, got)
}

// scopeAPI serves every real operation with a no-op handler behind the auth
// middleware (Pelican mode).
func scopeAPI(t *testing.T, log *slog.Logger) (*fiber.App, *auth) {
	a, err := newAuth(pelicanConfig(), log)
	require.NoError(t, err)
	fa, _, api := NewAPI("test", fiber.Config{})
	api.UseMiddleware(a.middleware(api))
	for _, op := range operations() {
		huma.Register(api, huma.Operation{OperationID: op.OperationID, Method: op.Method, Path: op.Path, Tags: op.Tags,
			Metadata: op.Metadata, DefaultStatus: http.StatusNoContent},
			func(context.Context, *struct{}) (*struct{}, error) { return nil, nil })
	}
	return fa, a
}

func bearerDo(t *testing.T, fa *fiber.App, method, path, tok string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := fa.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)
	return resp
}

func TestEachScopeOpensItsAreaOnly(t *testing.T) {
	var logs bytes.Buffer
	noTime := func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}
	fa, _ := scopeAPI(t, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{ReplaceAttr: noTime})))
	param := regexp.MustCompile(`\{[^}]+\}`)
	for _, scope := range allScopes {
		for _, op := range operations() {
			if op.Metadata[publicMeta] == true {
				continue
			}
			need, _ := requiredScope(op)
			path := "/api" + param.ReplaceAllString(op.Path, "1")
			resp := bearerDo(t, fa, op.Method, path, token(t, scope))
			if need == scope {
				require.Equal(t, http.StatusNoContent, resp.StatusCode, "%s with %s", op.OperationID, scope)
				continue
			}
			require.Equal(t, http.StatusForbidden, resp.StatusCode, "%s with %s", op.OperationID, scope)
			var p huma.ErrorModel
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&p))
			require.Equal(t, "permission denied: this needs the easy-pz."+need+" permission in Pelican", p.Detail)
		}
	}
	require.Equal(t, http.StatusNoContent, bearerDo(t, fa, http.MethodPut, "/api/settings", token(t, allScopes...)).StatusCode)
	require.Equal(t, http.StatusUnauthorized, bearerDo(t, fa, http.MethodGet, "/api/system", "").StatusCode)

	// Audit: writes with the Pelican user, and every refusal; allowed reads are not logged.
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	require.Contains(t, lines, `level=INFO msg="pelican action" user=alice sub=user-uuid via=bearer method=PUT path=/api/settings status=204`)
	require.Contains(t, lines, `level=WARN msg="pelican action refused" user=alice sub=user-uuid via=bearer method=POST path=/api/console/rcon need=console`)
	for _, l := range lines {
		if strings.Contains(l, `msg="pelican action"`) {
			require.NotContains(t, l, "method=GET", "reads are not audited")
		}
	}
}

// pelicanClient signs in through the handoff like the SPA: no cookies, the
// session token as a bearer.
type pelicanClient struct {
	t       *testing.T
	app     *fiber.App
	session string
}

func (c *pelicanClient) do(method, path, body string, hdr ...string) *http.Response {
	c.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.session != "" {
		req.Header.Set("Authorization", "Bearer "+c.session)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := c.app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(c.t, err)
	require.Empty(c.t, resp.Header.Values("Set-Cookie"), "%s %s: no cookie in Pelican mode", method, path)
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&v))
	return v
}

func TestPelicanHandoff(t *testing.T) {
	cfg := pelicanConfig()
	cfg.DataDir, cfg.InstallDir = t.TempDir(), t.TempDir() // the autosort below writes the ini
	c := &pelicanClient{t: t, app: newServer(t, cfg)}
	s := decode[sessionBody](t, c.do(http.MethodGet, "/api/auth/session", ""))
	require.False(t, s.Authenticated)
	require.Equal(t, "pelican", s.Host)
	require.Equal(t, "https://panel.example.com", s.PelicanPanelURL, "even when signed out: the login page links there")
	require.Equal(t, "https://panel.example.com/easy-pz/9c3b6a3e/open", s.PelicanOpenURL, "renewal: <panel>/easy-pz/<uuid_short>/open")
	require.Equal(t, []string{}, s.Scopes)
	require.Empty(t, s.Token)

	handoff := func(tok string) *http.Response {
		return c.do(http.MethodPost, "/api/auth/pelican", `{"token":"`+tok+`"}`)
	}
	tok := token(t, scopeRead, scopeMods)
	resp := handoff(tok)
	require.Equal(t, http.StatusOK, resp.StatusCode, "no CSRF: there is no ambient credential")
	s = decode[sessionBody](t, resp)
	require.True(t, s.Authenticated)
	require.Equal(t, "alice", s.Username)
	require.Equal(t, []string{scopeRead, scopeMods}, s.Scopes)
	require.NotEmpty(t, s.Token)
	require.WithinDuration(t, time.Now().Add(time.Hour), s.ExpiresAt, 5*time.Second, "one hour, not PANEL_SESSION_TTL")

	require.Equal(t, http.StatusUnauthorized, c.do(http.MethodGet, "/api/console/history", "").StatusCode, "no session yet")
	c.session = s.Token
	s = decode[sessionBody](t, c.do(http.MethodGet, "/api/auth/session", ""))
	require.True(t, s.Authenticated)
	require.Equal(t, []string{scopeRead, scopeMods}, s.Scopes, "the session keeps the scopes")
	require.Empty(t, s.Token, "only the sign-in answers the token")
	require.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/console/history", "").StatusCode, "the session token is reusable")
	require.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/console/history", "").StatusCode)
	require.Equal(t, http.StatusForbidden, c.do(http.MethodPut, "/api/settings", `{}`).StatusCode, "scopes still apply")
	require.Equal(t, http.StatusNoContent, c.do(http.MethodPost, "/api/mods/autosort", "").StatusCode, "a write without CSRF")

	// A cookie, even a valid-looking one, authenticates nothing.
	session := c.session
	c.session = ""
	require.Equal(t, http.StatusUnauthorized, c.do(http.MethodGet, "/api/console/history", "", "Cookie", sessionCookie+"="+session).StatusCode)
	c.session = session

	// Replayed, read-less, expired, wrong-server and forged tokens are refused.
	resp = handoff(tok)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Contains(t, decode[huma.ErrorModel](t, resp).Detail, "token already used")
	require.Equal(t, http.StatusForbidden, handoff(token(t, scopeMods)).StatusCode)
	old := tokenClaims(time.Now().Add(-2*time.Minute), scopeRead)
	require.Equal(t, http.StatusUnauthorized, handoff(mint(t, testPriv, old)).StatusCode)
	other := tokenClaims(time.Now(), scopeRead)
	other["aud"] = "pzman:00000000-0000-0000-0000-000000000000"
	require.Equal(t, http.StatusUnauthorized, handoff(mint(t, testPriv, other)).StatusCode)
	require.Equal(t, http.StatusUnauthorized, handoff(mint(t, otherPriv, tokenClaims(time.Now(), scopeRead))).StatusCode, "forged")

	// No password login on Pelican.
	resp = c.do(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"devpassword"}`)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Contains(t, decode[huma.ErrorModel](t, resp).Detail, "open easy-pz from the server's page in Pelican (https://panel.example.com)")
	require.Equal(t, http.StatusNoContent, c.do(http.MethodPost, "/api/auth/logout", "").StatusCode)
}

type sessionBody = struct {
	Authenticated   bool      `json:"authenticated"`
	Username        string    `json:"username"`
	Scopes          []string  `json:"scopes"`
	ExpiresAt       time.Time `json:"expiresAt"`
	Host            string    `json:"host"`
	PelicanPanelURL string    `json:"pelicanPanelUrl"`
	PelicanOpenURL  string    `json:"pelicanOpenUrl"`
	Token           string    `json:"token"`
}

// Pelican sessions end one hour after the handoff, however busy: a scope
// revoked in Pelican applies within that hour.
func TestPelicanSessionDoesNotSlide(t *testing.T) {
	fa, a := scopeAPI(t, slog.New(slog.DiscardHandler))
	start := time.Now()
	a.now = func() time.Time { return start }
	tok, exp, err := a.issue(principal{Subject: "user-uuid", Name: "alice", Scopes: []string{scopeRead}, Pelican: true})
	require.NoError(t, err)
	require.Equal(t, start.Add(time.Hour).Unix(), exp.Unix())
	for _, at := range []time.Duration{time.Minute, 31 * time.Minute, 59 * time.Minute} {
		a.now = func() time.Time { return start.Add(at) }
		resp := bearerDo(t, fa, http.MethodGet, "/api/system", tok)
		require.Equal(t, http.StatusNoContent, resp.StatusCode, at)
		require.Empty(t, resp.Header.Values("Set-Cookie"), "no renewed session")
	}
	a.now = func() time.Time { return start.Add(61 * time.Minute) }
	resp := bearerDo(t, fa, http.MethodGet, "/api/system", tok)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Contains(t, decode[huma.ErrorModel](t, resp).Detail, "the session ended")
}

func TestTickets(t *testing.T) {
	fa, a := scopeAPI(t, slog.New(slog.DiscardHandler))
	get := func(path string) int {
		resp, err := fa.Test(httptest.NewRequest(http.MethodGet, path, nil))
		require.NoError(t, err)
		return resp.StatusCode
	}
	reader := principal{Subject: "u", Name: "bob", Scopes: []string{scopeRead}, Pelican: true}
	backups := principal{Subject: "u", Name: "alice", Scopes: []string{scopeRead, scopeBackups}, Pelican: true}
	mk := func(p principal, path string) string {
		id, _, err := a.tickets.issue(p, path)
		require.NoError(t, err)
		return id
	}
	id := mk(reader, "/api/events")
	require.Equal(t, http.StatusNoContent, get("/api/events?ticket="+id))
	require.Equal(t, http.StatusUnauthorized, get("/api/events?ticket="+id), "single-use")
	require.Equal(t, http.StatusUnauthorized, get("/api/events?ticket=nope"))

	id = mk(backups, "/api/backups/1/download")
	require.Equal(t, http.StatusUnauthorized, get("/api/backups/2/download?ticket="+id), "bound to its path")
	require.Equal(t, http.StatusUnauthorized, get("/api/backups/1/download?ticket="+id), "a wrong use spends it too")
	require.Equal(t, http.StatusNoContent, get("/api/backups/1/download?ticket="+mk(backups, "/api/backups/1/download")))
	require.Equal(t, http.StatusForbidden, get("/api/backups/1/download?ticket="+mk(reader, "/api/backups/1/download")),
		"the ticket carries the session's scopes")
	require.Equal(t, http.StatusUnauthorized, get("/api/system?ticket="+mk(reader, "/api/system")), "only the stream and downloads take tickets")

	start := time.Now()
	id = mk(reader, "/api/events")
	a.now = func() time.Time { return start.Add(31 * time.Second) }
	require.Equal(t, http.StatusUnauthorized, get("/api/events?ticket="+id), "30 s")
}

// End to end: the SPA mints tickets with its session token.
func TestTicketEndpoint(t *testing.T) {
	c := &pelicanClient{t: t, app: newServer(t, pelicanConfig())}
	ticket := func(path string) *http.Response {
		return c.do(http.MethodPost, "/api/auth/ticket", `{"path":"`+path+`"}`)
	}
	require.Equal(t, http.StatusUnauthorized, ticket("/api/backups/1/download").StatusCode)
	c.session = decode[sessionBody](t, c.do(http.MethodPost, "/api/auth/pelican", `{"token":"`+token(t, scopeRead, scopeBackups)+`"}`)).Token
	require.Equal(t, http.StatusUnprocessableEntity, ticket("/api/system").StatusCode, "only ticket paths")
	resp := ticket("/api/backups/7/download")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	tk := decode[struct {
		Ticket    string    `json:"ticket"`
		ExpiresAt time.Time `json:"expiresAt"`
	}](t, resp)
	require.WithinDuration(t, time.Now().Add(30*time.Second), tk.ExpiresAt, 5*time.Second)
	c.session = ""
	require.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/backups/7/download?ticket="+tk.Ticket, "").StatusCode,
		"authenticated by the ticket; no such backup")
	require.Equal(t, http.StatusUnauthorized, c.do(http.MethodGet, "/api/backups/7/download?ticket="+tk.Ticket, "").StatusCode)
}

func TestBearerServiceCalls(t *testing.T) {
	c := &pelicanClient{t: t, app: newServer(t, pelicanConfig())}
	call := func(method, path, body, tok string) *http.Response {
		t.Helper()
		c.session = tok
		return c.do(method, path, body)
	}
	resp := call(http.MethodGet, "/api/server/status", "", token(t, scopeRead))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp = call(http.MethodGet, "/api/settings", "", token(t, scopeRead))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	settingsBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	tok := token(t, scopeRead, scopeSettings)
	require.Equal(t, http.StatusOK, call(http.MethodPut, "/api/settings", string(settingsBody), tok).StatusCode, "a write without CSRF")
	require.Equal(t, http.StatusUnauthorized, call(http.MethodPut, "/api/settings", string(settingsBody), tok).StatusCode, "single-use")
	s := decode[sessionBody](t, call(http.MethodGet, "/api/auth/session", "", token(t, scopeRead)))
	require.False(t, s.Authenticated, "the session endpoint does not take (or spend) Pelican tokens")

	expired := tokenClaims(time.Now().Add(-2*time.Minute), scopeRead)
	resp = call(http.MethodGet, "/api/server/status", "", mint(t, testPriv, expired))
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	p := decode[huma.ErrorModel](t, resp)
	require.Contains(t, p.Detail, "bearer token refused: ")
	require.Contains(t, p.Detail, "token is expired")
	wrong := tokenClaims(time.Now(), scopeRead)
	wrong["aud"] = "pzman:00000000-0000-0000-0000-000000000000"
	require.Equal(t, http.StatusUnauthorized, call(http.MethodGet, "/api/server/status", "", mint(t, testPriv, wrong)).StatusCode)
	require.Equal(t, http.StatusUnauthorized, call(http.MethodGet, "/api/server/status", "", "not.a.token").StatusCode)
	// A session token signed by another pzman (another server) is refused.
	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "x", "scp": allScopes,
		"exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte("another pzman"))
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, call(http.MethodGet, "/api/server/status", "", forged).StatusCode)
}

func TestDockerSessionHasEveryScope(t *testing.T) {
	c := newClient(t, testConfig())
	var s SessionOutput
	resp := c.do(http.MethodGet, "/api/auth/session", "")
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&s.Body))
	require.Equal(t, "docker", s.Body.Host)
	require.Empty(t, s.Body.PelicanPanelURL)

	require.Equal(t, http.StatusForbidden, c.do(http.MethodPost, "/api/auth/pelican", `{"token":"`+token(t, scopeRead)+`"}`,
		csrfHeader, c.cookies[csrfCookie]).StatusCode, "no Pelican sign-in in Docker mode")
	// A bearer header neither authenticates nor skips CSRF in Docker mode.
	require.Equal(t, http.StatusUnauthorized, c.do(http.MethodGet, "/api/system", "", "Authorization", "Bearer "+token(t, scopeRead)).StatusCode)
	require.Equal(t, http.StatusForbidden, c.do(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"devpassword"}`,
		"Authorization", "Bearer x").StatusCode)

	c.login()
	resp = c.do(http.MethodGet, "/api/auth/session", "")
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&s.Body))
	require.True(t, s.Body.Authenticated)
	require.Equal(t, "admin", s.Body.Username)
	require.Equal(t, allScopes, s.Body.Scopes)
}

// A Pelican session token carries its scopes and its expiry (an hour); a
// Docker session token (no scp claim) grants nothing in Pelican mode.
func TestPelicanSessionTokenClaims(t *testing.T) {
	a, err := newAuth(pelicanConfig(), slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	start := time.Now().Truncate(time.Second)
	a.now = func() time.Time { return start }
	p := principal{Subject: "user-uuid", Name: "alice", Scopes: []string{scopeRead, scopeBackups}, Pelican: true}
	tok, exp, err := a.issue(p)
	require.NoError(t, err)
	require.Equal(t, start.Add(time.Hour), exp)
	a.now = func() time.Time { return start.Add(59 * time.Minute) }
	got, _, err := a.verify(tok)
	require.NoError(t, err)
	p.Expires = exp
	require.Equal(t, p, got)

	d, err := newAuth(testConfig(), slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	d.secret = a.secret
	dtok, _, err := d.issue(principal{Subject: "admin"})
	require.NoError(t, err)
	got, _, err = a.verify(dtok)
	require.NoError(t, err)
	require.Empty(t, got.Scopes)
}
