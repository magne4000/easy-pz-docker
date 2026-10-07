package pzclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/magne4000/easy-pz-docker/internal/publicapi"
)

func TestPageURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://pz.example.com/mods/abc":            "https://pz.example.com/mods/abc/",
		" https://pz.example.com/mods/abc/ ":         "https://pz.example.com/mods/abc/",
		"https://pz.example.com/mods/abc/data.json":  "https://pz.example.com/mods/abc/",
		"http://10.0.0.2:8080/mods/abc/?x=1#frag":    "http://10.0.0.2:8080/mods/abc/",
		"https://example.com/proxy/pz/mods/abc/":     "https://example.com/proxy/pz/mods/abc/",
		"https://pz.example.com/mods/abc/index.html": "https://pz.example.com/mods/abc/",
	} {
		got, err := PageURL(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "pz.example.com/mods/abc", "ftp://x/mods/a/", "https://pz.example.com/", "https://pz.example.com/mods/"} {
		_, err := PageURL(in)
		require.Error(t, err, in)
	}
}

func TestFetchAndDownload(t *testing.T) {
	payload := []byte("zip bytes")
	sum := sha256.Sum256(payload)
	players := 3
	data := publicapi.PublicData{ServerName: "srv", Status: "available", Players: &players,
		Connect:     &publicapi.PublicConnect{Host: "pz.example.com", Port: 16261},
		GameVersion: "42.21",
		Items: []publicapi.PublicItem{{WorkshopID: "1", Download: publicapi.PublicPack{
			URL: "/mods/tok/download/1.zip", Ready: true, SHA256: hex.EncodeToString(sum[:])}}}}
	mux := http.NewServeMux()
	mux.HandleFunc("/mods/tok/data.json", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(data) })
	mux.HandleFunc("/mods/tok/download/1.zip", func(w http.ResponseWriter, r *http.Request) { w.Write(payload) })
	mux.HandleFunc("/mods/tok/download/2.zip", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New()
	ctx := context.Background()
	page := srv.URL + "/mods/tok/"

	got, err := c.Fetch(ctx, page)
	require.NoError(t, err)
	require.Equal(t, "42.21", got.GameVersion)
	require.Equal(t, 16261, got.Connect.Port)

	_, err = c.Fetch(ctx, srv.URL+"/mods/wrong/")
	require.ErrorContains(t, err, "not found")

	var buf bytes.Buffer
	var last int64
	require.NoError(t, c.Download(ctx, page, got.Items[0].Download.URL, got.Items[0].Download.SHA256, &buf, func(n int64) { last = n }))
	require.Equal(t, payload, buf.Bytes())
	require.Equal(t, int64(len(payload)), last)

	buf.Reset()
	require.ErrorContains(t, c.Download(ctx, page, got.Items[0].Download.URL, "00", &buf, nil), "checksum")
	require.ErrorIs(t, c.Download(ctx, page, "/mods/tok/download/2.zip", "", &buf, nil), ErrNotReady)

	path, err := c.DownloadFile(ctx, t.TempDir(), page, got.Items[0].Download.URL, got.Items[0].Download.SHA256, nil)
	require.NoError(t, err)
	require.FileExists(t, path)
}

func TestBusyAnswersAreWaitedOut(t *testing.T) {
	var calls atomic.Int32
	sel := publicapi.FilesRequest{SHA256: "s", Files: []int{0, 2}}
	mux := http.NewServeMux()
	// One 429, then the answer; the retried POST must carry its body again.
	mux.HandleFunc("POST /mods/tok/files/1", func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		var got publicapi.FilesRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		require.Equal(t, sel, got)
		w.Write([]byte("zip"))
	})
	mux.HandleFunc("/mods/tok/data.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	mux.HandleFunc("/mods/tok/files/2", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusConflict) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New()
	ctx := context.Background()
	page := srv.URL + "/mods/tok/"

	path, err := c.DownloadFiles(ctx, t.TempDir(), page, "/mods/tok/files/1", sel, nil)
	require.NoError(t, err)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "zip", string(b))
	require.Equal(t, int32(2), calls.Load())

	// Status polls give up rather than outwait their timeout.
	_, err = c.Fetch(ctx, page)
	require.ErrorIs(t, err, ErrBusy)

	c.BusyWait = 0
	calls.Store(0)
	_, err = c.DownloadFiles(ctx, t.TempDir(), page, "/mods/tok/files/1", sel, nil)
	require.ErrorIs(t, err, ErrBusy)

	_, err = c.DownloadFiles(ctx, t.TempDir(), page, "/mods/tok/files/2", sel, nil)
	require.ErrorIs(t, err, ErrChanged)
}
