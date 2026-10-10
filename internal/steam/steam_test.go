package steam

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const appManifest = `"AppState"
{
	"appid"		"380870"
	"StateFlags"		"4"
	"buildid"		"24909836"
	"LastUpdated"		"1757000000"
	"SizeOnDisk"		"123"
	"UserConfig" { "BetaKey" "unstable" }
	"MountedConfig" { "BetaKey" "public" }
}`

const wsManifest = `"AppWorkshop"
{
	"appid"		"108600"
	"WorkshopItemsInstalled"
	{
		"2169435993"
		{
			"size"		"31729"
			"timeupdated"		"1700000000"
			"manifest"		"123"
		}
	}
}`

const appInfo = `Redirecting stderr to '/root/Steam/logs/stderr.txt'
[  0%] Checking for available updates...
Connecting anonymously to Steam Public...OK
AppID : 380870, change number : 1/2, last change : Mon Sep 15
"380870"
{
	"common" { "name" "Project Zomboid Dedicated Server" "desc" "has \"quotes\" {" }
	"depots"
	{
		"branches"
		{
			"public" { "buildid" "24909836" "timeupdated" "1757000000" }
			"unstable" { "buildid" "25000000" }
		}
	}
}
Unloading Steam API...OK`

func TestManifests(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadAppManifest(dir)
	require.ErrorIs(t, err, os.ErrNotExist)
	ws, err := ReadWorkshopManifest(dir)
	require.NoError(t, err)
	require.Empty(t, ws)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "steamapps", "workshop"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "steamapps", "appmanifest_380870.acf"), []byte(appManifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "steamapps", "workshop", "appworkshop_108600.acf"), []byte(wsManifest), 0o644))
	m, err := ReadAppManifest(dir)
	require.NoError(t, err)
	require.Equal(t, "24909836", m.BuildID)
	require.Equal(t, "unstable", m.Branch)
	ws, err = ReadWorkshopManifest(dir)
	require.NoError(t, err)
	require.Equal(t, int64(31729), ws["2169435993"].Size)
	require.Equal(t, int64(1700000000), ws["2169435993"].TimeUpdated.Unix())
}

func TestParseAppInfo(t *testing.T) {
	id, err := ParseAppInfoBuildID(appInfo, "")
	require.NoError(t, err)
	require.Equal(t, "24909836", id)
	id, err = ParseAppInfoBuildID(appInfo, "unstable")
	require.NoError(t, err)
	require.Equal(t, "25000000", id)
	_, err = ParseAppInfoBuildID(appInfo, "nope")
	require.Error(t, err)
}

func fakeRun(transcript string, delay time.Duration) runFunc {
	return func(ctx context.Context, args []string) (io.ReadCloser, func() error, error) {
		pr, pw := io.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			for _, l := range strings.SplitAfter(transcript, "\n") {
				select {
				case <-ctx.Done():
					pw.Close()
					return
				case <-time.After(delay):
				}
				pw.Write([]byte(l))
			}
			<-ctx.Done()
			pw.Close()
		}()
		return pr, func() error { <-done; return ctx.Err() }, nil
	}
}

func newTest(transcript string, stall time.Duration) (*SteamCMD, *[]Progress) {
	s := NewSteamCMD(Options{InstallDir: "/x", Stall: stall, UID: -1})
	s.run = func(ctx context.Context, args []string) (io.ReadCloser, func() error, error) {
		pr, pw := io.Pipe()
		go func() { io.WriteString(pw, transcript); pw.Close() }()
		return pr, func() error { return nil }, nil
	}
	var ps []Progress
	return s, &ps
}

func TestAppUpdateParsing(t *testing.T) {
	s, ps := newTest("Connecting anonymously to Steam Public...OK\r\n\x1b[0m Update state (0x61) downloading, progress: 45.23 (1 / 2)\r Update state (0x81) committing, progress: 99.00 (2 / 2)\nSuccess! App '380870' fully installed.\n", time.Second)
	require.NoError(t, s.AppUpdate(context.Background(), "", true, func(p Progress) { *ps = append(*ps, p) }))
	require.Equal(t, 45.23, (*ps)[1].Percent)
	require.Equal(t, "committing", (*ps)[2].Phase)

	s, _ = newTest("Error! App '380870' state is 0x202 after update job.\n", time.Second)
	require.ErrorContains(t, s.AppUpdate(context.Background(), "", false, nil), "0x202")

	s, _ = newTest("ERROR! Failed to install app '380870' (Disk write failure)\n", time.Second)
	require.ErrorContains(t, s.AppUpdate(context.Background(), "", false, nil), "Disk write failure")

	s, _ = newTest("Loading Steam API...OK\nSomething unexpected\n", time.Second)
	require.ErrorContains(t, s.AppUpdate(context.Background(), "", false, nil), "Something unexpected")

	s, _ = newTest("Success. Downloaded item 1 to \"/x\" (10 bytes)\n", time.Second)
	s.o.Guard = func() error { return errors.New("server running") }
	require.ErrorContains(t, s.AppUpdate(context.Background(), "", false, nil), "server running")
	// Adding a mod while the server runs must still download it.
	require.NoError(t, s.WorkshopDownload(context.Background(), []WorkshopItem{{ID: "1"}}, nil))
}

func TestAppUpdateRetriesMissingConfiguration(t *testing.T) {
	transcripts := []string{
		"ERROR! Failed to install app '380870' (Missing configuration)\n",
		"Success! App '380870' fully installed.\n",
	}
	s, _ := newTest("", time.Second)
	calls := 0
	s.run = func(ctx context.Context, args []string) (io.ReadCloser, func() error, error) {
		pr, pw := io.Pipe()
		tr := transcripts[min(calls, len(transcripts)-1)]
		calls++
		go func() { io.WriteString(pw, tr); pw.Close() }()
		return pr, func() error { return nil }, nil
	}
	require.NoError(t, s.AppUpdate(context.Background(), "", false, nil))
	require.Equal(t, 2, calls)

	// Only one retry: a persistent failure still surfaces.
	calls = 0
	transcripts = transcripts[:1]
	require.ErrorContains(t, s.AppUpdate(context.Background(), "", false, nil), "Missing configuration")
	require.Equal(t, 2, calls)
}

func TestWorkshopDownloadParsing(t *testing.T) {
	// The real transcript's shape: messages run together on one line.
	s, ps := newTest("Downloading item 1 ...\nSuccess. Downloaded item 1 to \"/x\" (10 bytes) Downloading item 2 ...\nERROR! Download item 2 failed (Failure).\n", time.Second)
	err := s.WorkshopDownload(context.Background(), []WorkshopItem{{ID: "1"}, {ID: "2"}}, func(p Progress) { *ps = append(*ps, p) })
	require.ErrorContains(t, err, "2 (Failure)")
	require.NotContains(t, err.Error(), "1 (")
	var msgs []string
	for _, p := range *ps {
		msgs = append(msgs, p.Message)
	}
	require.Equal(t, []string{"Connecting to Steam", "Downloading item 1 (1/2)", "Downloaded item 1 (1/2)", "Downloading item 2 (2/2)"}, msgs)
}

// steamcmd prints nothing between "Downloading item" and "Success": the bytes
// it stages are the progress, and they keep the watchdog off a slow download.
func TestWorkshopDownloadProgressFromStagedBytes(t *testing.T) {
	install := t.TempDir()
	var mu sync.Mutex
	var console []string
	s := NewSteamCMD(Options{InstallDir: install, Stall: 300 * time.Millisecond, UID: -1,
		OnLine: func(l string) { mu.Lock(); console = append(console, l); mu.Unlock() }})
	s.pollEvery = 20 * time.Millisecond
	staging := workshopStagingDir(install, "1")
	s.run = func(ctx context.Context, args []string) (io.ReadCloser, func() error, error) {
		pr, pw := io.Pipe()
		go func() {
			defer pw.Close()
			io.WriteString(pw, "Downloading item 1 ...\n")
			if err := os.MkdirAll(staging, 0o755); err != nil {
				t.Error(err)
			}
			for i := 1; i <= 6; i++ { // silent for twice the stall timeout
				time.Sleep(100 * time.Millisecond)
				if err := os.WriteFile(filepath.Join(staging, "data"), make([]byte, i*100), 0o644); err != nil {
					t.Error(err)
				}
			}
			io.WriteString(pw, "Success. Downloaded item 1 to \"/x\" (1000 bytes)\n")
		}()
		return pr, func() error { return nil }, nil
	}
	var ps []Progress
	err := s.WorkshopDownload(context.Background(), []WorkshopItem{{ID: "1", Title: "Brita", Size: 1000}},
		func(p Progress) { mu.Lock(); ps = append(ps, p); mu.Unlock() })
	require.NoError(t, err)

	var partial bool
	for i, p := range ps {
		if i > 0 {
			require.GreaterOrEqual(t, p.Percent, ps[i-1].Percent, "progress never goes back")
		}
		if p.Percent > 0 && p.Percent < 100 {
			partial = true
			require.Regexp(t, `^Downloading Brita \(1/1\): \d+ B of 1000 B$`, p.Message)
		}
	}
	require.True(t, partial, "staged bytes show between start and success: %+v", ps)
	require.Equal(t, Progress{Phase: "workshop", Percent: 100, Message: "Downloaded Brita (1/1)"}, ps[len(ps)-1])
	require.Contains(t, strings.Join(console, "\n"), "Downloading item 1: ")
}

// Without every size the percentage counts items, never dividing by an unknown size.
func TestWorkshopPercentWithUnknownSize(t *testing.T) {
	install := t.TempDir()
	stage := func(id string, n int) {
		dir := workshopStagingDir(install, id)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), make([]byte, n), 0o644))
	}
	var last Progress
	tr := newWsTracker([]WorkshopItem{{ID: "1", Size: 300}, {ID: "2"}}, func(p Progress) { last = p }, nil)
	stage("1", 150)
	require.True(t, tr.measure(install))
	require.Equal(t, 25.0, last.Percent, "half of the first of two items")
	tr.finish("1")
	require.Equal(t, 50.0, last.Percent)
	stage("2", 2048)
	require.True(t, tr.measure(install))
	require.Equal(t, Progress{Phase: "workshop", Percent: 50, Message: "Downloading item 2 (2/2): 2.0 KB"}, last)
	require.False(t, tr.measure(install), "unchanged bytes are no progress")
}

func TestStallWatchdog(t *testing.T) {
	s := NewSteamCMD(Options{InstallDir: "/x", Stall: 100 * time.Millisecond, UID: -1})
	s.run = fakeRun("Connecting...\n", time.Millisecond)
	start := time.Now()
	err := s.AppUpdate(context.Background(), "", false, nil)
	require.ErrorIs(t, err, ErrStalled)
	require.Less(t, time.Since(start), 2*time.Second)
}

func TestWebAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch {
		case strings.Contains(r.URL.Path, "GetPublishedFileDetails"):
			require.Equal(t, "2", r.Form.Get("itemcount"))
			io.WriteString(w, `{"response":{"publishedfiledetails":[{"publishedfileid":"1","result":1,"title":"Brita","file_size":"3088000000","time_updated":1700000000,"tags":[{"tag":"Build 42"}]},{"publishedfileid":"2","result":9}]}}`)
		case strings.Contains(r.URL.Path, "GetCollectionDetails") && r.Form.Get("publishedfileids[0]") == "7":
			// Steam's real answer for a plain item id (3378285185) or a private collection
			io.WriteString(w, `{"response":{"result":1,"resultcount":0,"collectiondetails":[{"publishedfileid":"7","result":9}]}}`)
		case strings.Contains(r.URL.Path, "GetCollectionDetails"):
			io.WriteString(w, `{"response":{"collectiondetails":[{"result":1,"children":[{"publishedfileid":"b","sortorder":2,"filetype":0},{"publishedfileid":"a","sortorder":1,"filetype":0},{"publishedfileid":"c","sortorder":3,"filetype":2}]}]}}`)
		}
	}))
	defer srv.Close()
	api := NewWebAPI(srv.Client(), srv.URL)
	ds, err := api.PublishedFileDetails(context.Background(), []string{"1", "2"})
	require.NoError(t, err)
	require.Equal(t, int64(3088000000), ds[0].FileSize)
	require.Equal(t, []string{"Build 42"}, ds[0].Tags)
	ids, err := api.CollectionDetails(context.Background(), "99")
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, ids)
	_, err = api.CollectionDetails(context.Background(), "7")
	require.ErrorIs(t, err, ErrCollectionNotFound)
}
