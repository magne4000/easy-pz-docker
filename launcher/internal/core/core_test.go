package core

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/magne4000/easy-pz-docker/internal/publicapi"
	"github.com/magne4000/easy-pz-docker/launcher/helpermod"
	"github.com/magne4000/easy-pz-docker/launcher/internal/config"
	"github.com/magne4000/easy-pz-docker/launcher/internal/game"
	"github.com/magne4000/easy-pz-docker/launcher/internal/pzhash"
	"github.com/magne4000/easy-pz-docker/launcher/internal/zomboid"
)

type events struct {
	mu  sync.Mutex
	got []Phase
}

func (e *events) Emit(name string, data any) {
	if p, ok := data.(Phase); ok && name == EventPlayPhase {
		e.mu.Lock()
		e.got = append(e.got, p)
		e.mu.Unlock()
	}
}

func (e *events) phases() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, p := range e.got {
		if len(out) == 0 || out[len(out)-1] != p.Phase {
			out = append(out, p.Phase)
		}
	}
	return out
}

// fakeServer is a pzman public mod page with one mod item.
type fakeServer struct {
	*httptest.Server
	mu     sync.Mutex
	status []string // successive statuses, the last one sticks
}

func newFakeServer(t *testing.T) *fakeServer {
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("ModA/42/mod.info")
	w.Write([]byte("id=ModA"))
	zw.Close()
	sum := sha256.Sum256(zbuf.Bytes())
	f := &fakeServer{status: []string{"available"}}
	mux := http.NewServeMux()
	mux.HandleFunc("/mods/tok/data.json", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		st := f.status[0]
		if len(f.status) > 1 {
			f.status = f.status[1:]
		}
		f.mu.Unlock()
		json.NewEncoder(w).Encode(publicapi.PublicData{ServerName: "Test Server", Status: st, StatusMessage: st,
			Connect:     &publicapi.PublicConnect{Host: "pz.example.com", Port: 16261},
			GameVersion: "42.21",
			Items: []publicapi.PublicItem{{WorkshopID: "1", Title: "Mod A",
				Mods:     []publicapi.PublicMod{{ID: "ModA", Folder: "ModA"}},
				Download: publicapi.PublicPack{URL: "/mods/tok/download/1.zip", Ready: true, SHA256: hex.EncodeToString(sum[:])}}}})
	})
	mux.HandleFunc("/mods/tok/download/1.zip", func(w http.ResponseWriter, r *http.Request) { w.Write(zbuf.Bytes()) })
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func fakeInstall(t *testing.T) string {
	dir := t.TempDir()
	for _, p := range []string{"projectzomboid.sh", "ProjectZomboid64.exe", "projectzomboid.jar",
		"Project Zomboid.app/Contents/Info.plist", "Project Zomboid.app/Contents/Java/projectzomboid.jar"} {
		full := filepath.Join(dir, filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, nil, 0o755))
	}
	return dir
}

type harness struct {
	svc     *Service
	user    zomboid.Dir
	ev      *events
	srv     *fakeServer
	started int
}

func newHarness(t *testing.T) *harness {
	h := &harness{user: zomboid.Dir(t.TempDir()), ev: &events{}, srv: newFakeServer(t)}
	store, err := config.Open(filepath.Join(t.TempDir(), "config.json"))
	require.NoError(t, err)
	require.NoError(t, store.Update(func(c *config.Config) error { c.GameDir = fakeInstall(t); return nil }))
	h.svc = New("test", store, h.user, h.ev, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.svc.start = func(game.Install) error { h.started++; return nil }
	h.svc.PollInterval, h.svc.RetryInterval, h.svc.ConsumeTimeout = time.Millisecond, time.Millisecond, 50*time.Millisecond
	return h
}

func (h *harness) page() string { return h.srv.URL + "/mods/tok/" }

func saveAccount(t *testing.T, user zomboid.Dir, host string, port int, username, hash string) int64 {
	path := user.ServerListDB()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE server (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, ip TEXT NOT NULL,
		port INTEGER NOT NULL, serverPassword TEXT, description TEXT);
		CREATE TABLE account (id INTEGER PRIMARY KEY AUTOINCREMENT, serverId INTEGER NOT NULL, username TEXT NOT NULL,
		password TEXT, isSavePassword INTEGER DEFAULT 0, authType INTEGER DEFAULT 1, lastLogon TEXT);`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO server (name, ip, port, serverPassword) VALUES ('Saved', ?, ?, 'savedsrvpw')`, host, port)
	require.NoError(t, err)
	res, err := db.Exec(`INSERT INTO account (serverId, username, password, isSavePassword, lastLogon) VALUES (1, ?, ?, 1, '2026-09-01 10:00:00')`, username, hash)
	require.NoError(t, err)
	id, _ := res.LastInsertId()
	return id
}

func readIni(t *testing.T, user zomboid.Dir) string {
	b, err := os.ReadFile(user.AutoConnectFile())
	require.NoError(t, err)
	return string(b)
}

func TestAddServerAndStatus(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	srv, err := h.svc.AddServerURL(ctx, h.page()+"data.json")
	require.NoError(t, err)
	require.Equal(t, "Test Server", srv.Name)
	require.Equal(t, "pz.example.com", srv.Host)
	require.Equal(t, 16261, srv.Port)

	_, err = h.svc.AddServerURL(ctx, h.page())
	require.ErrorContains(t, err, "already in your list")

	st, err := h.svc.Status(ctx, srv.ID)
	require.NoError(t, err)
	require.True(t, st.Reachable)
	require.Equal(t, "42.21", st.GameVersion)
	require.Len(t, st.Plan.Download, 1)
	require.Nil(t, st.Saved)

	require.NoError(t, h.svc.Sync(ctx, srv.ID, false))
	st, err = h.svc.Status(ctx, srv.ID)
	require.NoError(t, err)
	require.Empty(t, st.Plan.Download)
	require.Equal(t, 1, st.Plan.UpToDate)
}

func TestPlayWithTypedPassword(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	srv, err := h.svc.AddServerURL(ctx, h.page())
	require.NoError(t, err)

	require.ErrorContains(t, h.svc.Play(ctx, srv.ID, PlayRequest{}), "username and password")
	require.Zero(t, h.started)

	require.NoError(t, h.svc.Play(ctx, srv.ID, PlayRequest{Username: " alice ", Password: "secret", ServerPassword: "srvpw"}))
	require.Equal(t, 1, h.started)
	require.Equal(t, []string{PhaseSyncing, PhaseLaunching, PhaseLaunched}, h.ev.phases())
	ini := readIni(t, h.user)
	require.Contains(t, ini, "host=pz.example.com\nport=16261\nuser=alice\npassword="+pzhash.Hash("secret")+"\ndoHash=false\n")
	require.Contains(t, ini, "serverPassword=srvpw\n")
	require.NotContains(t, ini, "secret\n", "no plain-text password on disk")
	require.DirExists(t, filepath.Join(h.user.Mods(), "ModA"))
	require.DirExists(t, filepath.Join(h.user.Mods(), helpermod.ID))
	active, err := os.ReadFile(filepath.Join(h.user.Mods(), "default.txt"))
	require.NoError(t, err)
	require.Contains(t, string(active), "mod = "+helpermod.ID+",")

	// Nobody consumes the file: it is removed after ConsumeTimeout.
	require.Eventually(t, func() bool {
		_, err := os.Stat(h.user.AutoConnectFile())
		return errors.Is(err, os.ErrNotExist)
	}, 2*time.Second, 10*time.Millisecond)
}

func TestPlayWithSavedAccount(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	accID := saveAccount(t, h.user, "pz.example.com", 16261, "bob", "$2a$12$storedhash")
	srv, err := h.svc.AddServerURL(ctx, h.page())
	require.NoError(t, err)

	st, err := h.svc.Status(ctx, srv.ID)
	require.NoError(t, err)
	require.NotNil(t, st.Saved)
	require.Equal(t, "bob", st.Saved.Accounts[0].Username)

	require.ErrorContains(t, h.svc.Play(ctx, srv.ID, PlayRequest{AccountID: accID + 1}), "no longer saved")
	require.NoError(t, h.svc.Play(ctx, srv.ID, PlayRequest{AccountID: accID}))
	ini := readIni(t, h.user)
	require.Contains(t, ini, "user=bob\npassword=$2a$12$storedhash\n")
	require.Contains(t, ini, "serverPassword=savedsrvpw\n")
	got, _ := h.svc.server(srv.ID)
	require.Equal(t, "bob", got.Account, "remembered as the default account")
}

func TestPlayWaitsForServer(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	srv, err := h.svc.AddServerURL(ctx, h.page())
	require.NoError(t, err)
	h.srv.mu.Lock()
	h.srv.status = []string{"restarting", "restarting", "restarting", "available"}
	h.srv.mu.Unlock()
	require.NoError(t, h.svc.Play(ctx, srv.ID, PlayRequest{Username: "a", Password: "b"}))
	require.Equal(t, []string{PhaseSyncing, PhaseWaiting, PhaseLaunching, PhaseLaunched}, h.ev.phases())

	h.srv.mu.Lock()
	h.srv.status = []string{"unavailable"}
	h.srv.mu.Unlock()
	cctx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, h.svc.Play(cctx, srv.ID, PlayRequest{Username: "a", Password: "b"}), context.DeadlineExceeded)
	require.Equal(t, 1, h.started, "cancelled before launch")
}

func TestStartGameClearsLeftoverFile(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	srv, err := h.svc.AddServerURL(ctx, h.page())
	require.NoError(t, err)
	require.NoError(t, h.user.WriteAutoConnect(zomboid.AutoConnect{Host: "x", Port: 1, User: "u"}))
	require.NoError(t, h.svc.StartGame(ctx, srv.ID, false))
	require.Equal(t, 1, h.started)
	_, err = os.Stat(h.user.AutoConnectFile())
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestForeignModsNeedTakeOver(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	srv, err := h.svc.AddServerURL(ctx, h.page())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(h.user.Mods(), "ModA"), 0o755))
	err = h.svc.Sync(ctx, srv.ID, false)
	require.ErrorIs(t, err, ErrForeignMods)
	require.True(t, strings.HasSuffix(err.Error(), ": ModA"), err.Error())
	require.NoError(t, h.svc.Sync(ctx, srv.ID, true))
}

func TestImportSaved(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	saveAccount(t, h.user, "10.0.0.5", 16300, "carol", "$2a$12$x")
	saved, err := h.svc.SavedServers(ctx)
	require.NoError(t, err)
	require.Len(t, saved, 1)

	srv, err := h.svc.ImportSaved(ctx, saved[0].ID, "")
	require.NoError(t, err)
	require.Equal(t, config.Server{ID: srv.ID, Name: "Saved", Host: "10.0.0.5", Port: 16300, Account: "carol"}, srv)
	st, err := h.svc.Status(ctx, srv.ID)
	require.NoError(t, err)
	require.False(t, st.HasModPage)
	require.NotNil(t, st.Saved)

	_, err = h.svc.ImportSaved(ctx, 999, "")
	require.Error(t, err)

	edited, err := h.svc.UpdateServer(srv.ID, "Renamed", "10.0.0.6", 16301, "")
	require.NoError(t, err)
	require.Equal(t, "Renamed", edited.Name)
	require.NoError(t, h.svc.RemoveServer(srv.ID))
	require.Empty(t, h.svc.Servers())
}
