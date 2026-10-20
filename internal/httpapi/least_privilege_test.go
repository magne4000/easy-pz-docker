package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

// signIn hands a token with scopes over and returns a client holding its session.
func signIn(t *testing.T, c *pelicanClient, scopes ...string) *pelicanClient {
	t.Helper()
	s := decode[sessionBody](t, (&pelicanClient{t: t, app: c.app}).do(http.MethodPost, "/api/auth/pelican",
		`{"token":"`+token(t, scopes...)+`"}`))
	require.NotEmpty(t, s.Token)
	return &pelicanClient{t: t, app: c.app, session: s.Token}
}

func TestIniRCONPasswordNeedsConfig(t *testing.T) {
	cfg := pelicanConfig()
	cfg.DataDir = t.TempDir()
	p := filepath.Join(cfg.DataDir, "Server", cfg.ServerName+".ini")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte("PVP=true\nRCONPassword=s3cret\n"), 0o644))
	base := &pelicanClient{t: t, app: newServer(t, cfg)}
	entries := func(c *pelicanClient) map[string]IniEntryView {
		resp := c.do(http.MethodGet, "/api/config/ini", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		out := map[string]IniEntryView{}
		for _, e := range decode[struct {
			Entries []IniEntryView `json:"entries"`
		}](t, resp).Entries {
			out[e.Key] = e
		}
		return out
	}
	ro := entries(signIn(t, base, scopeRead))
	require.Equal(t, IniEntryView{Key: "RCONPassword", Redacted: true}, ro["RCONPassword"])
	require.Equal(t, "true", ro["PVP"].Value, "other values are not withheld")
	require.Equal(t, "s3cret", entries(signIn(t, base, scopeRead, scopeConfig))["RCONPassword"].Value)

	docker := testConfig()
	docker.DataDir = cfg.DataDir
	c := newClient(t, docker)
	c.login()
	resp := c.do(http.MethodGet, "/api/config/ini", "")
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(b), `"value":"s3cret"`, "the admin has every scope")
}

// The schedules scope manages schedules, but a schedule's action needs its
// own scope: schedules alone cannot restart the server or take backups.
func TestScheduleActionScopes(t *testing.T) {
	base := &pelicanClient{t: t, app: newServer(t, pelicanConfig())}
	sched := signIn(t, base, scopeRead, scopeSchedules)
	body := func(action string) string {
		msg := ""
		if action == "broadcast" {
			msg = `,"message":"hi"`
		}
		return fmt.Sprintf(`{"name":"n","action":%q,"cron":"0 4 * * *","enabled":false%s}`, action, msg)
	}
	refused := func(resp *http.Response, need string) {
		t.Helper()
		require.Equal(t, http.StatusForbidden, resp.StatusCode)
		require.Contains(t, decode[huma.ErrorModel](t, resp).Detail, fmt.Sprintf("also needs the %q scope", need))
	}
	for action, need := range map[string]string{"restart": scopeControl, "save": scopeControl, "broadcast": scopeControl,
		"backup": scopeBackups} {
		refused(sched.do(http.MethodPost, "/api/schedules", body(action)), need)
	}

	both := signIn(t, base, scopeRead, scopeSchedules, scopeBackups)
	resp := both.do(http.MethodPost, "/api/schedules", body("backup"))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	id := decode[struct {
		ID int64 `json:"id"`
	}](t, resp).ID
	path := fmt.Sprintf("/api/schedules/%d", id)

	refused(sched.do(http.MethodPut, path, body("backup")), scopeBackups)
	refused(sched.do(http.MethodPost, path+"/run", ""), scopeBackups)
	refused(both.do(http.MethodPut, path, body("restart")), scopeControl)
	require.Equal(t, http.StatusOK, both.do(http.MethodPut, path, body("backup")).StatusCode)
	require.Equal(t, http.StatusNoContent, sched.do(http.MethodDelete, path, "").StatusCode, "deleting needs schedules only")

	all := signIn(t, base, allScopes...)
	require.Equal(t, http.StatusOK, all.do(http.MethodPost, "/api/schedules", body("restart")).StatusCode)
}
