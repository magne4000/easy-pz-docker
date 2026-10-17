package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// Only the upload streams past the body limit; it stays inside the mods
// folder and replaces a local mod only on request.
func TestUploadLocalMod(t *testing.T) {
	cfg := testConfig()
	cfg.DataDir, cfg.InstallDir = t.TempDir(), t.TempDir()
	c := newClient(t, cfg)
	c.login()
	modsDir := filepath.Join(cfg.DataDir, "mods")
	upload := func(query string, files ...[2]string) int {
		t.Helper()
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		for _, f := range files {
			require.NoError(t, w.WriteField("path", f[0]))
			fw, err := w.CreateFormFile("file", "blob")
			require.NoError(t, err)
			_, err = fw.Write([]byte(f[1]))
			require.NoError(t, err)
		}
		require.NoError(t, w.Close())
		req := httptest.NewRequest(http.MethodPost, "/api/mods/local"+query, &b)
		req.Header.Set("Content-Type", w.FormDataContentType())
		req.Header.Set(csrfHeader, c.cookies[csrfCookie])
		for k, v := range c.cookies {
			req.AddCookie(&http.Cookie{Name: k, Value: v})
		}
		resp, err := c.app.Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
		require.NoError(t, err)
		resp.Body.Close()
		return resp.StatusCode
	}
	big := strings.Repeat("x", 5<<20) // over the 4 MB body limit

	require.Equal(t, http.StatusOK, upload("", [2]string{"Mine/common/mod.info", "id=Mine\n"}, [2]string{"Mine/common/media/big.txt", big}))
	st, err := os.Stat(filepath.Join(modsDir, "Mine", "common", "media", "big.txt"))
	require.NoError(t, err)
	require.Equal(t, int64(len(big)), st.Size())

	require.Equal(t, http.StatusConflict, upload("", [2]string{"Mine/common/mod.info", "id=Mine\n"}))
	require.Equal(t, http.StatusOK, upload("?replace=true", [2]string{"Mine/common/mod.info", "id=Mine\nname=v2\n"}))
	require.NoFileExists(t, filepath.Join(modsDir, "Mine", "common", "media", "big.txt"), "a replacement is the new folder as a whole")

	for _, files := range [][][2]string{
		{{"../Evil/common/mod.info", "id=Evil"}},
		{{"Evil/../../x/common/mod.info", "id=Evil"}},
		{{"Evil/common/mod.info", "id=Evil"}, {"Other/common/mod.info", "id=Other"}},
		{{"Evil/readme.txt", "no mod.info"}},
		{{".Evil/common/mod.info", "id=Evil"}},
	} {
		require.Equal(t, http.StatusUnprocessableEntity, upload("", files...), files[0][0])
	}
	entries, err := os.ReadDir(modsDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "refused uploads leave nothing behind")
	require.NoDirExists(t, filepath.Join(cfg.DataDir, "Evil"))

	resp := c.do(http.MethodPut, "/api/mods/order", `{"modIds":["`+big+`"]}`, csrfHeader, c.cookies[csrfCookie])
	require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}
