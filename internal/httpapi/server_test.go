package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/app"
	"github.com/magne4000/easy-pz-docker/internal/backup"
	"github.com/magne4000/easy-pz-docker/internal/console"
	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/sched"
	"github.com/magne4000/easy-pz-docker/internal/settings"
	"github.com/magne4000/easy-pz-docker/internal/steam"
	"github.com/magne4000/easy-pz-docker/internal/store"
	"github.com/magne4000/easy-pz-docker/internal/webui"
)

// password "devpassword"
const testHash = "$2a$04$1R5soAXs9bxJJinRr4Y2KupxylWP0IvMkZPWMcjaGvd.ANQfcm1JW" // bcrypt cost 4: fast tests; production requires >= 10

func testConfig() app.Config {
	return app.Config{Env: "prod", AdminUser: "admin", AdminHash: testHash, JWTSecret: "test-secret",
		SessionTTL: 3600e9, CookieSecure: app.CookieSecureAuto, ServerName: "s", Drivers: app.DriversFake}
}

func newServer(t *testing.T, cfg app.Config) *fiber.App {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, slog.Default(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	bus := events.NewBus(slog.Default(), 16)
	st, err := settings.Open(ctx, cfg, db, bus)
	require.NoError(t, err)
	ring := console.NewRing(10)
	ring.Append("hello")
	s, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), Deps{Cfg: cfg, Bus: bus, Ring: ring, Store: db, Settings: st})
	require.NoError(t, err)
	return s.App()
}

type client struct {
	t       *testing.T
	app     *fiber.App
	cookies map[string]string
}

func (c *client) do(method, path, body string, hdr ...string) *http.Response {
	c.t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	for k, v := range c.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	resp, err := c.app.Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
	require.NoError(c.t, err)
	for _, ck := range resp.Cookies() {
		c.cookies[ck.Name] = ck.Value
	}
	return resp
}

func (c *client) login() {
	c.do(http.MethodGet, "/api/auth/session", "")
	resp := c.do(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"devpassword"}`, csrfHeader, c.cookies[csrfCookie])
	require.Equal(c.t, http.StatusOK, resp.StatusCode)
}

func newClient(t *testing.T, cfg app.Config) *client {
	return &client{t: t, app: newServer(t, cfg), cookies: map[string]string{}}
}

func TestEveryDocumentedPathIsRoutedByHuma(t *testing.T) {
	_, _, api := NewAPI("test", fiber.Config{})
	RegisterRoutes(api, Deps{})
	RegisterAuthRoutes(api)
	c := newClient(t, testConfig())
	param := regexp.MustCompile(`\{[^}]+\}`)
	for path, item := range api.OpenAPI().Paths {
		url := "/api" + param.ReplaceAllString(path, "1")
		for method, op := range map[string]*huma.Operation{"GET": item.Get, "POST": item.Post, "PUT": item.Put, "DELETE": item.Delete} {
			if op == nil || op.Metadata[publicMeta] == true {
				continue
			}
			resp := c.do(method, url, "", csrfHeader, c.cookies[csrfCookie])
			// Not logged in: Huma's auth middleware must answer, never the SPA fallback.
			if method == "GET" {
				require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "%s %s", method, url)
			}
			require.Contains(t, resp.Header.Get("Content-Type"), "json", "%s %s", method, url)
		}
	}
}

func TestUnknownAPIPathIsProblem(t *testing.T) {
	c := newClient(t, testConfig())
	resp := c.do(http.MethodGet, "/api/nope", "")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, "application/problem+json", resp.Header.Get("Content-Type"))
}

func TestAuthFlow(t *testing.T) {
	c := newClient(t, testConfig())
	require.Equal(t, http.StatusUnauthorized, c.do(http.MethodGet, "/api/console/history", "").StatusCode)

	c.do(http.MethodGet, "/api/auth/session", "")
	// no CSRF header: rejected before the handler
	resp := c.do(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"devpassword"}`)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	resp = c.do(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"nope"}`, csrfHeader, c.cookies[csrfCookie])
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Empty(t, c.cookies[sessionCookie])

	c.login()
	resp = c.do(http.MethodGet, "/api/console/history?limit=5", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body struct {
		Lines []events.ConsoleLine `json:"lines"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "hello", body.Lines[0].Text)
}

func TestLoginRateLimited(t *testing.T) {
	c := newClient(t, testConfig())
	c.do(http.MethodGet, "/api/auth/session", "")
	var last int
	for range 7 {
		last = c.do(http.MethodPost, "/api/auth/login", `{"username":"admin","password":"x"}`, csrfHeader, c.cookies[csrfCookie]).StatusCode
	}
	require.Equal(t, http.StatusTooManyRequests, last)
}

func TestValidationLocation(t *testing.T) {
	c := newClient(t, testConfig())
	c.login()
	resp := c.do(http.MethodGet, "/api/console/history?limit=99999", "")
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	var p huma.ErrorModel
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&p))
	require.Equal(t, "query.limit", p.Errors[0].Location)
}

func secureCookie(t *testing.T, cfg app.Config, hdr ...string) bool {
	c := newClient(t, cfg)
	c.do(http.MethodGet, "/api/auth/session", "")
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"devpassword"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeader, c.cookies[csrfCookie])
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: c.cookies[csrfCookie]})
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := c.app.Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	for _, ck := range resp.Cookies() {
		if ck.Name == sessionCookie {
			return ck.Secure
		}
	}
	t.Fatal("no session cookie")
	return false
}

func TestCookieSecure(t *testing.T) {
	cfg := testConfig()
	// fiber's app.Test connects from 0.0.0.0
	require.False(t, secureCookie(t, cfg, "X-Forwarded-Proto", "https"), "untrusted peer must not flip Secure")
	cfg.TrustedProxies = []string{"0.0.0.0"}
	require.True(t, secureCookie(t, cfg, "X-Forwarded-Proto", "https", "Origin", "https://example.com"))
	require.False(t, secureCookie(t, cfg))
	cfg.TrustedProxies = nil
	cfg.CookieSecure = app.CookieSecureTrue
	require.True(t, secureCookie(t, cfg))
}

func TestStaticAndDevSkip(t *testing.T) {
	if !webui.Available() {
		t.Skip("no UI build embedded")
	}
	c := newClient(t, testConfig())
	resp := c.do(http.MethodGet, "/settings", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
	require.Contains(t, resp.Header.Get("Content-Type"), "text/html")

	dev := testConfig()
	dev.Env = "dev"
	c = newClient(t, dev)
	require.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/", "").StatusCode)
}

func TestPublicPageNeedsToken(t *testing.T) {
	cfg := testConfig()
	cfg.ModsToken = "secret-token"
	c := newClient(t, cfg)
	require.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/mods/wrong/data.json", "").StatusCode)
}

func TestMapErr(t *testing.T) {
	var se huma.StatusError
	for err, code := range map[error]int{
		store.ErrNotFound:         404,
		&sched.BusyError{Op: "x"}: 409,
		backup.ErrBusy:            409,
		backup.ErrMissing:         410,
		fmt.Errorf("%w: 3378285185", steam.ErrCollectionNotFound): 422,
		errors.New("boom"):          500,
		huma.Error403Forbidden("x"): 403,
	} {
		require.ErrorAs(t, mapErr(err), &se)
		require.Equal(t, code, se.GetStatus(), err.Error())
	}
}

// The UI's "back up even if nothing changed" checkbox sends force=false; Huma
// must not overwrite it with the default.
func TestCreateBackupHonoursForceFalse(t *testing.T) {
	_, api := humatest.New(t)
	var got []bool
	huma.Register(api, huma.Operation{Method: http.MethodPost, Path: "/backups"}, func(_ context.Context, in *CreateBackupInput) (*struct{}, error) {
		got = append(got, in.force())
		return nil, nil
	})
	api.Post("/backups", map[string]any{"force": false})
	api.Post("/backups", map[string]any{"force": true})
	api.Post("/backups", map[string]any{"note": "x"})
	require.Equal(t, []bool{false, true, true}, got)
}

// PZ pauses an empty server unless the .ini says otherwise: only an explicit
// PauseEmpty=false may raise the "backups are never skipped" warning.
func TestPauseEmptyDefaultsToTrue(t *testing.T) {
	cfg := testConfig()
	cfg.DataDir = t.TempDir()
	d := Deps{Cfg: cfg}
	require.True(t, pauseEmpty(d), "no .ini")
	for ini, want := range map[string]bool{"Public=true\n": true, "PauseEmpty=true\n": true, "PauseEmpty=false\n": false} {
		require.NoError(t, os.MkdirAll(filepath.Dir(iniPath(d)), 0o755))
		require.NoError(t, os.WriteFile(iniPath(d), []byte(ini), 0o644))
		require.Equal(t, want, pauseEmpty(d), ini)
	}
}

func TestSandboxEndpoints(t *testing.T) {
	cfg := testConfig()
	cfg.DataDir = t.TempDir()
	c := newClient(t, cfg)
	c.login()
	put := func(body string) *http.Response {
		return c.do(http.MethodPut, "/api/config/sandbox", body, csrfHeader, c.cookies[csrfCookie])
	}

	resp := c.do(http.MethodGet, "/api/config/sandbox", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out SandboxOutput
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out.Body))
	require.False(t, out.Body.Exists)
	require.Equal(t, http.StatusNotFound, put(`{"values":{"Zombies":"3"}}`).StatusCode)

	src := "SandboxVars = {\n    -- Minimum=0.00 Maximum=4.00 Default=1.00\n    Rate = 1.0,\n    Flag = false,\n}\n"
	p := filepath.Join(cfg.DataDir, "Server", "s_SandboxVars.lua")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(src), 0o644))

	resp = c.do(http.MethodGet, "/api/config/sandbox", "")
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out.Body))
	require.Len(t, out.Body.Entries, 2)
	require.Equal(t, 4.0, *out.Body.Entries[0].Max)

	require.Equal(t, http.StatusUnprocessableEntity, put(`{"values":{"Rate":"9"}}`).StatusCode)
	require.Equal(t, http.StatusUnprocessableEntity, put(`{"values":{"Nope":"1"}}`).StatusCode)
	b, _ := os.ReadFile(p)
	require.Equal(t, src, string(b), "a rejected update writes nothing")

	resp = put(`{"values":{"Rate":"2.5","Flag":"false"}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var upd SandboxUpdateOutput
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&upd.Body))
	require.Equal(t, []string{"Rate"}, upd.Body.Changed)
	b, _ = os.ReadFile(p)
	require.Equal(t, strings.Replace(src, "Rate = 1.0", "Rate = 2.5", 1), string(b))
}
