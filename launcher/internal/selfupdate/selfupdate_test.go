package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssetName(t *testing.T) {
	require.Equal(t, "easypz-launcher-windows-amd64.exe", AssetName("windows", "amd64"))
	require.Equal(t, "easypz-launcher-linux-arm64", AssetName("linux", "arm64"))
	require.Equal(t, "easypz-launcher-darwin-universal.zip", AssetName("darwin", "arm64"))
}

func TestParseChecksums(t *testing.T) {
	got := parseChecksums([]byte("aa  easypz-launcher-linux-amd64\nbb *checksums.txt\n\nbad line here\n"))
	require.Equal(t, map[string]string{"easypz-launcher-linux-amd64": "aa", "checksums.txt": "bb"}, got)
}

func releaseServer(t *testing.T, tag string, payload []byte, sum string) *httptest.Server {
	mux := http.NewServeMux()
	var srv *httptest.Server
	name := AssetName(runtime.GOOS, runtime.GOARCH)
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "html_url": "https://example/r",
			"assets": []map[string]string{
				{"name": name, "browser_download_url": srv.URL + "/dl/bin"},
				{"name": checksums, "browser_download_url": srv.URL + "/dl/sums"},
			}})
	})
	mux.HandleFunc("/dl/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(payload) })
	mux.HandleFunc("/dl/sums", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sum + "  " + name + "\n")) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestCheck(t *testing.T) {
	srv := releaseServer(t, "v1.2.0", nil, "")
	ctx := context.Background()
	for cur, newer := range map[string]bool{"v1.1.9": true, "v1.2.0": false, "v1.3.0": false, "dev": false} {
		u := New(cur)
		u.API = srv.URL
		rel, err := u.Check(ctx)
		require.NoError(t, err, cur)
		require.Equal(t, newer, rel != nil, cur)
		if newer {
			require.Equal(t, "v1.2.0", rel.Version)
		}
	}
}

func TestDownloadVerifiesChecksum(t *testing.T) {
	payload := []byte("new launcher")
	sum := sha256.Sum256(payload)
	srv := releaseServer(t, "v2.0.0", payload, hex.EncodeToString(sum[:]))
	u := New("v1.0.0")
	u.API = srv.URL
	dir := t.TempDir()

	p, err := u.download(context.Background(), srv.URL+"/dl/bin", dir, hex.EncodeToString(sum[:]))
	require.NoError(t, err)
	got, _ := os.ReadFile(p)
	require.Equal(t, payload, got)

	_, err = u.download(context.Background(), srv.URL+"/dl/bin", dir, "00")
	require.ErrorContains(t, err, "checksum")
}

func TestReplace(t *testing.T) {
	dir := t.TempDir()
	exe, src := filepath.Join(dir, "launcher"), filepath.Join(dir, "new")
	require.NoError(t, os.WriteFile(exe, []byte("old"), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o755))
	require.NoError(t, replace(src, exe))
	got, _ := os.ReadFile(exe)
	require.Equal(t, "new", string(got))
	require.NoFileExists(t, exe+".old")
}
