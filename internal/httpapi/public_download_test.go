package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/publicapi"
	"github.com/magne4000/easy-pz-docker/internal/steam"
)

func TestParseRange(t *testing.T) {
	type r struct {
		start, length int64
		ok            bool
	}
	for spec, want := range map[string]r{
		"bytes=0-9":     {0, 10, true},
		"bytes=90-":     {90, 10, true},
		"bytes=-10":     {90, 10, true},
		"bytes=-200":    {0, 100, true},
		"bytes=95-200":  {95, 5, true},
		"bytes=100-":    {0, 0, true}, // unsatisfiable
		"bytes=-0":      {0, 0, true},
		"bytes=0-1,5-6": {0, 0, false},
		"bytes=5-1":     {0, 0, false},
		"items=0-1":     {0, 0, false},
		"bytes=x-":      {0, 0, false},
	} {
		s, l, ok := parseRange(spec, 100)
		require.Equal(t, want, r{s, l, ok}, spec)
	}
}

// serve runs the app on a real socket: slots are released when fasthttp
// closes the body, which app.Test does not exercise.
func serve(t *testing.T, a *fiber.App) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go a.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true})
	t.Cleanup(func() { a.Shutdown() })
	return "http://" + ln.Addr().String()
}

func TestPublicDownloads(t *testing.T) {
	cfg := testConfig()
	cfg.DataDir, cfg.InstallDir, cfg.ModsToken = t.TempDir(), t.TempDir(), "tok"
	a, db := newServerDB(t, cfg)
	mod := filepath.Join(steam.WorkshopContentDir(cfg.InstallDir, "100"), "mods", "ModA", "42")
	// Big enough to fill the socket buffers, so a reader that stops keeps the
	// transfer (and its slot) open.
	big := make([]byte, 32<<20)
	rand.NewChaCha8([32]byte{}).Read(big)
	for name, content := range map[string][]byte{"mod.info": []byte("id=ModA\n"), "media/a.lua": []byte("a"), "big.bin": big} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(mod, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(mod, name), content, 0o644))
	}
	_, err := db.AddItem(context.Background(), "100", time.Now())
	require.NoError(t, err)
	base := serve(t, a) + "/mods/tok/"
	do := func(method, path, body string, hdr ...string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		require.NoError(t, err)
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		return resp
	}
	read := func(resp *http.Response) []byte {
		t.Helper()
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return b
	}

	var l publicapi.PackFiles
	require.Eventually(t, func() bool {
		resp := do(http.MethodGet, "files/100", "")
		b := read(resp)
		return resp.StatusCode == http.StatusOK && json.Unmarshal(b, &l) == nil
	}, 10*time.Second, 50*time.Millisecond)
	paths := []string{}
	for _, f := range l.Files {
		paths = append(paths, f.Path)
	}
	require.Equal(t, []string{"ModA/42/big.bin", "ModA/42/media/a.lua", "ModA/42/mod.info"}, paths)

	resp := do(http.MethodGet, "download/100.zip", "")
	full := read(resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	sum := sha256.Sum256(full)
	require.Equal(t, l.SHA256, hex.EncodeToString(sum[:]))
	require.Equal(t, l.SHA256, resp.Header.Get("X-Checksum-SHA256"))
	tag := resp.Header.Get("ETag")
	require.Equal(t, `"`+l.SHA256+`"`, tag)

	// Resuming: a range of the same pack, the whole pack once it changed, 416 past the end.
	resp = do(http.MethodGet, "download/100.zip", "", "Range", "bytes=10-19", "If-Range", tag)
	require.Equal(t, http.StatusPartialContent, resp.StatusCode)
	require.Equal(t, "bytes 10-19/"+strconv.Itoa(len(full)), resp.Header.Get("Content-Range"))
	require.Equal(t, full[10:20], read(resp))
	resp = do(http.MethodGet, "download/100.zip", "", "Range", "bytes=10-19", "If-Range", `"old"`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, read(resp), len(full))
	resp = do(http.MethodGet, "download/100.zip", "", "Range", "bytes="+strconv.Itoa(len(full))+"-")
	read(resp)
	require.Equal(t, http.StatusRequestedRangeNotSatisfiable, resp.StatusCode)

	// Partial download: only the requested entries, as a zip.
	files := func(sha string, idx ...int) *http.Response {
		b, _ := json.Marshal(publicapi.FilesRequest{SHA256: sha, Files: idx})
		return do(http.MethodPost, "files/100", string(b), "Content-Type", "application/json")
	}
	resp = files(l.SHA256, 1, 2, 1)
	body := read(resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)
	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)
		b, err := io.ReadAll(rc)
		require.NoError(t, err)
		got[f.Name] = string(b)
	}
	require.Equal(t, map[string]string{"ModA/42/media/a.lua": "a", "ModA/42/mod.info": "id=ModA\n"}, got)
	for _, tc := range []struct {
		resp *http.Response
		code int
	}{
		{files("stale", 1), http.StatusConflict},
		{files(l.SHA256, 3), http.StatusBadRequest},
		{files(l.SHA256), http.StatusBadRequest},
		{do(http.MethodGet, "files/all", ""), http.StatusNotFound},
	} {
		read(tc.resp)
		require.Equal(t, tc.code, tc.resp.StatusCode)
	}

	// Slots are held while bodies are written: the 5th concurrent transfer
	// waits, and abandoned transfers free theirs.
	var open []*http.Response
	for range downloadSlots {
		resp := do(http.MethodGet, "download/100.zip", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		open = append(open, resp)
	}
	resp = do(http.MethodGet, "files/100", "")
	read(resp)
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	require.Equal(t, "5", resp.Header.Get("Retry-After"))
	for _, r := range open {
		r.Body.Close()
	}
	require.Eventually(t, func() bool {
		resp := do(http.MethodGet, "files/100", "")
		read(resp)
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond)
}
