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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
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
		"get-session": public, "login": public, "login-pelican": public, "logout": public,

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
func scopeAPI(t *testing.T, log *slog.Logger) *fiber.App {
	a, err := newAuth(pelicanConfig(), log)
	require.NoError(t, err)
	fa, _, api := NewAPI("test", fiber.Config{})
	api.UseMiddleware(a.middleware(api))
	for _, op := range operations() {
		huma.Register(api, huma.Operation{OperationID: op.OperationID, Method: op.Method, Path: op.Path, Tags: op.Tags,
			Metadata: op.Metadata, DefaultStatus: http.StatusNoContent},
			func(context.Context, *struct{}) (*struct{}, error) { return nil, nil })
	}
	return fa
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
	fa := scopeAPI(t, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{ReplaceAttr: noTime})))
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

func TestPelicanHandoff(t *testing.T) {
	c := newClient(t, pelicanConfig())
	var s SessionOutput
	resp := c.do(http.MethodGet, "/api/auth/session", "")
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&s.Body))
	require.False(t, s.Body.Authenticated)
	require.Equal(t, "pelican", s.Body.Host)
	require.Equal(t, "https://panel.example.com", s.Body.PelicanPanelURL, "even when signed out: the login page links there")
	require.Equal(t, []string{}, s.Body.Scopes)

	handoff := func(tok string, hdr ...string) *http.Response {
		return c.do(http.MethodPost, "/api/auth/pelican", `{"token":"`+tok+`"}`, hdr...)
	}
	tok := token(t, scopeRead, scopeMods)
	require.Equal(t, http.StatusForbidden, handoff(tok).StatusCode, "CSRF as for the login")
	resp = handoff(tok, csrfHeader, c.cookies[csrfCookie])
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&s.Body))
	require.True(t, s.Body.Authenticated)
	require.Equal(t, "alice", s.Body.Username)
	require.Equal(t, []string{scopeRead, scopeMods}, s.Body.Scopes)
	require.NotEmpty(t, c.cookies[sessionCookie])

	resp = c.do(http.MethodGet, "/api/auth/session", "")
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&s.Body))
	require.Equal(t, []string{scopeRead, scopeMods}, s.Body.Scopes, "the session keeps the scopes")
	require.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/console/history", "").StatusCode)
	require.Equal(t, http.StatusForbidden, c.do(http.MethodPut, "/api/settings", `{}`, csrfHeader, c.cookies[csrfCookie]).StatusCode)

	// Replayed, read-less, expired and wrong-server tokens are refused.
	resp = handoff(tok, csrfHeader, c.cookies[csrfCookie])
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	var p huma.ErrorModel
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&p))
	require.Contains(t, p.Detail, "token already used")
	require.Equal(t, http.StatusForbidden, handoff(token(t, scopeMods), csrfHeader, c.cookies[csrfCookie]).StatusCode)
	old := tokenClaims(time.Now().Add(-2*time.Minute), scopeRead)
	require.Equal(t, http.StatusUnauthorized, handoff(mint(t, testPriv, old), csrfHeader, c.cookies[csrfCookie]).StatusCode)
	other := tokenClaims(time.Now(), scopeRead)
	other["aud"] = "pzman:00000000-0000-0000-0000-000000000000"
	require.Equal(t, http.StatusUnauthorized, handoff(mint(t, testPriv, other), csrfHeader, c.cookies[csrfCookie]).StatusCode)
	require.Equal(t, http.StatusUnauthorized, handoff(mint(t, otherPriv, tokenClaims(time.Now(), scopeRead)), csrfHeader,
		c.cookies[csrfCookie]).StatusCode, "forged")

	// No password login on Pelican.
	resp = c.do(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"devpassword"}`, csrfHeader, c.cookies[csrfCookie])
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&p))
	require.Contains(t, p.Detail, "open easy-pz from the server's page in Pelican (https://panel.example.com)")
}

func TestBearerServiceCalls(t *testing.T) {
	a := newServer(t, pelicanConfig())
	call := func(method, path, body, tok string, cookies ...*http.Cookie) *http.Response {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		for _, ck := range cookies {
			req.AddCookie(ck)
		}
		resp, err := a.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
		require.NoError(t, err)
		return resp
	}
	resp := call(http.MethodGet, "/api/server/status", "", token(t, scopeRead))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Empty(t, resp.Cookies(), "no session, no CSRF cookie")

	resp = call(http.MethodGet, "/api/settings", "", token(t, scopeRead))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	settingsBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	tok := token(t, scopeRead, scopeSettings)
	resp = call(http.MethodPut, "/api/settings", string(settingsBody), tok)
	require.Equal(t, http.StatusOK, resp.StatusCode, "a write without CSRF")
	require.Empty(t, resp.Cookies())
	require.Equal(t, http.StatusUnauthorized, call(http.MethodPut, "/api/settings", string(settingsBody), tok).StatusCode, "single-use")

	resp = call(http.MethodPut, "/api/settings", string(settingsBody), token(t, scopeRead, scopeSettings),
		&http.Cookie{Name: sessionCookie, Value: "anything"})
	require.Equal(t, http.StatusForbidden, resp.StatusCode, "with a session cookie, CSRF applies")

	expired := tokenClaims(time.Now().Add(-2*time.Minute), scopeRead)
	resp = call(http.MethodGet, "/api/server/status", "", mint(t, testPriv, expired))
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	var p huma.ErrorModel
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&p))
	require.Contains(t, p.Detail, "bearer token refused: ")
	require.Contains(t, p.Detail, "token is expired")
	wrong := tokenClaims(time.Now(), scopeRead)
	wrong["aud"] = "pzman:00000000-0000-0000-0000-000000000000"
	require.Equal(t, http.StatusUnauthorized, call(http.MethodGet, "/api/server/status", "", mint(t, testPriv, wrong)).StatusCode)
	require.Equal(t, http.StatusUnauthorized, call(http.MethodGet, "/api/server/status", "", "not.a.token").StatusCode)
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

// Sessions slide forward with their scopes.
func TestPelicanSessionRefreshKeepsScopes(t *testing.T) {
	cfg := pelicanConfig()
	a, err := newAuth(cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	p := principal{Subject: "user-uuid", Name: "alice", Scopes: []string{scopeRead, scopeBackups}, Pelican: true}
	tok, _, err := a.issue(p)
	require.NoError(t, err)
	a.now = func() time.Time { return time.Now().Add(cfg.SessionTTL * 3 / 4) }
	got, _, err := a.verify(tok)
	require.NoError(t, err)
	require.Equal(t, p, got)
	tok2, _, err := a.issue(got)
	require.NoError(t, err)
	got, _, err = a.verify(tok2)
	require.NoError(t, err)
	require.True(t, slices.Equal(p.Scopes, got.Scopes))

	// A Docker session (no scp claim) grants nothing in Pelican mode.
	d, err := newAuth(testConfig(), slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	d.secret = a.secret
	dtok, _, err := d.issue(principal{Subject: "admin"})
	require.NoError(t, err)
	got, _, err = a.verify(dtok)
	require.NoError(t, err)
	require.Empty(t, got.Scopes)
}
