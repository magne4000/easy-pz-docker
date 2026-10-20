package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/app"
	"github.com/magne4000/easy-pz-docker/internal/publicapi"
	"github.com/magne4000/easy-pz-docker/internal/steam"
	"github.com/magne4000/easy-pz-docker/internal/webui"
)

// publicServer serves one enabled Workshop mod on the public page "tok".
func publicServer(t *testing.T, cfg app.Config) *fiber.App {
	t.Helper()
	cfg.DataDir, cfg.InstallDir, cfg.ModsToken = t.TempDir(), t.TempDir(), "tok"
	a, db := newServerDB(t, cfg)
	mod := filepath.Join(steam.WorkshopContentDir(cfg.InstallDir, "100"), "mods", "ModA", "42")
	require.NoError(t, os.MkdirAll(mod, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mod, "mod.info"), []byte("id=ModA\n"), 0o644))
	_, err := db.AddItem(context.Background(), "100", time.Now())
	require.NoError(t, err)
	return a
}

func get(t *testing.T, a *fiber.App, path string, hdr ...string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := a.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)
	return resp
}

func TestPublicURLsFollowPublicURL(t *testing.T) {
	data := func(cfg app.Config) publicapi.PublicData {
		resp := get(t, publicServer(t, cfg), "/mods/tok/data.json")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var d publicapi.PublicData
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&d))
		require.Len(t, d.Items, 1)
		return d
	}
	links := func(d publicapi.PublicData) []string {
		return []string{d.PageURL, d.Items[0].Download.URL, d.Items[0].Files, d.Pack.URL}
	}

	docker := testConfig()
	docker.PublicURL = "https://pz.example.com/"
	require.Equal(t, []string{"https://pz.example.com/mods/tok/", "/mods/tok/download/100.zip", "/mods/tok/files/100",
		"/mods/tok/download/all.zip"}, links(data(docker)), "Docker mode: unchanged")

	pel := pelicanConfig()
	pel.PublicURL = "https://panel.example.com/easy-pz/abcd1234"
	require.Equal(t, []string{"https://panel.example.com/easy-pz/abcd1234/mods/tok/", "/easy-pz/abcd1234/mods/tok/download/100.zip",
		"/easy-pz/abcd1234/mods/tok/files/100", "/easy-pz/abcd1234/mods/tok/download/all.zip"}, links(data(pel)))

	if !webui.Available() {
		t.Skip("no UI build embedded")
	}
	built, err := fs.ReadFile(webui.FS(), "mods.html")
	require.NoError(t, err)
	page := func(cfg app.Config) string {
		resp := get(t, publicServer(t, cfg), "/mods/tok/")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return string(b)
	}
	require.Equal(t, string(built), page(docker), "Docker mode: the file as built")
	html := page(pel)
	require.Contains(t, html, `src="/easy-pz/abcd1234/assets/`)
	require.Contains(t, html, `href="/easy-pz/abcd1234/assets/`)
	require.NotContains(t, html, `="/assets/`)
	require.NotContains(t, html, `="/favicon.svg"`, "the panel proxies only mods/ and assets/")
	require.Contains(t, html, `href="data:image/svg+xml;base64,`)
	require.NotRegexp(t, `(src|href)="/[^e]`, html, "no URL outside the prefix")
	require.Equal(t, strings.Count(string(built), `="/assets/`), strings.Count(html, `="/easy-pz/abcd1234/assets/`))
}

// Behind Pelican, rate limits key on the player's address when the plugin
// signed the request, and on the connection's otherwise.
func TestPublicRateLimitBehindPelican(t *testing.T) {
	a := publicServer(t, pelicanConfig())
	const path = "/mods/tok/data.json"
	signed := func(ip string) []string {
		now := time.Now().Unix()
		return []string{"X-Forwarded-For", ip, proxyHeader,
			signProxy(testPriv, now, http.MethodGet, path)}
	}
	for range 60 {
		require.Equal(t, http.StatusOK, get(t, a, path, signed("203.0.113.7")...).StatusCode)
	}
	require.Equal(t, http.StatusTooManyRequests, get(t, a, path, signed("203.0.113.7")...).StatusCode)
	require.Equal(t, http.StatusOK, get(t, a, path, signed("203.0.113.8")...).StatusCode, "another player, another budget")

	for range 60 {
		require.Equal(t, http.StatusOK, get(t, a, path, "X-Forwarded-For", "198.51.100.1").StatusCode)
	}
	require.Equal(t, http.StatusTooManyRequests, get(t, a, path, "X-Forwarded-For", "198.51.100.2").StatusCode,
		"unsigned: X-Forwarded-For is ignored, every such request shares the connection's budget")
}
