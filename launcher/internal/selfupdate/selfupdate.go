package selfupdate

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	Repo         = "magne4000/easy-pz-docker"
	checksums    = "checksums.txt"
	assetPrefix  = "easypz-launcher-"
	MacAppBundle = "easypz-launcher.app"
)

type Release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	asset   asset
	sums    asset
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func AssetName(goos, goarch string) string {
	switch goos {
	case "windows":
		return assetPrefix + "windows-" + goarch + ".exe"
	case "darwin":
		return assetPrefix + "darwin-universal.zip"
	default:
		return assetPrefix + goos + "-" + goarch
	}
}

type Updater struct {
	Current string
	HTTP    *http.Client
	API     string
}

func New(current string) *Updater {
	return &Updater{Current: current, HTTP: &http.Client{Timeout: 5 * time.Minute}, API: "https://api.github.com"}
}

func (u *Updater) Check(ctx context.Context) (*Release, error) {
	if !semver.IsValid(u.Current) {
		return nil, nil // dev build
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.API+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("check for updates: GitHub answered %s", resp.Status)
	}
	var gh struct {
		Tag    string  `json:"tag_name"`
		URL    string  `json:"html_url"`
		Assets []asset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gh); err != nil {
		return nil, err
	}
	if !semver.IsValid(gh.Tag) || semver.Compare(gh.Tag, u.Current) <= 0 {
		return nil, nil
	}
	rel := &Release{Version: gh.Tag, URL: gh.URL}
	want := AssetName(runtime.GOOS, runtime.GOARCH)
	for _, a := range gh.Assets {
		switch a.Name {
		case want:
			rel.asset = a
		case checksums:
			rel.sums = a
		}
	}
	if rel.asset.URL == "" || rel.sums.URL == "" {
		return nil, nil
	}
	return rel, nil
}

// Apply returns the path to start to run the new version.
func (u *Updater) Apply(ctx context.Context, rel *Release) (string, error) {
	sums, err := u.fetch(ctx, rel.sums.URL, 1<<20)
	if err != nil {
		return "", err
	}
	want, ok := parseChecksums(sums)[rel.asset.Name]
	if !ok {
		return "", fmt.Errorf("no checksum for %s", rel.asset.Name)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return u.applyMac(ctx, rel, want, exe)
	}
	tmp, err := u.download(ctx, rel.asset.URL, filepath.Dir(exe), want)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	if err := os.Chmod(tmp, 0o755); err != nil {
		return "", err
	}
	return exe, replace(tmp, exe)
}

// A running executable can be renamed (even on Windows) but not overwritten.
func replace(src, dst string) error {
	old := dst + ".old"
	os.Remove(old)
	if err := os.Rename(dst, old); err != nil {
		return fmt.Errorf("replace the launcher: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		os.Rename(old, dst)
		return fmt.Errorf("replace the launcher: %w", err)
	}
	os.Remove(old) // fails on Windows while running; CleanupOld removes it next start
	return nil
}

func CleanupOld() {
	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old")
	}
}

func (u *Updater) applyMac(ctx context.Context, rel *Release, want, exe string) (string, error) {
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if filepath.Ext(bundle) != ".app" {
		return "", errors.New("the launcher is not running from its .app bundle; download the update manually")
	}
	parent := filepath.Dir(bundle)
	zipPath, err := u.download(ctx, rel.asset.URL, parent, want)
	if err != nil {
		return "", err
	}
	defer os.Remove(zipPath)
	tmp, err := os.MkdirTemp(parent, ".easypz-update-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err := unzip(zipPath, tmp); err != nil {
		return "", err
	}
	newBundle := filepath.Join(tmp, MacAppBundle)
	if _, err := os.Stat(newBundle); err != nil {
		return "", fmt.Errorf("the update does not contain %s", MacAppBundle)
	}
	old := bundle + ".old"
	os.RemoveAll(old)
	if err := os.Rename(bundle, old); err != nil {
		return "", err
	}
	if err := os.Rename(newBundle, bundle); err != nil {
		os.Rename(old, bundle)
		return "", err
	}
	os.RemoveAll(old)
	return bundle, nil
}

func (u *Updater) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func (u *Updater) download(ctx context.Context, url, dir, want string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download update: %s", resp.Status)
	}
	f, err := os.CreateTemp(dir, ".easypz-update-*")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want) {
		err = errors.New("the update is corrupted (checksum mismatch)")
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func parseChecksums(data []byte) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 {
			out[strings.TrimPrefix(f[1], "*")] = f[0]
		}
	}
	return out
}

func unzip(zipPath, dst string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	root, _ := filepath.Abs(dst)
	for _, f := range zr.File {
		target := filepath.Join(root, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe path in update: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeZipEntry(f, target); err != nil {
			return err
		}
	}
	return nil
}

func writeZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	if f.Mode()&os.ModeSymlink != 0 {
		// Keep app bundle symlinks inside the bundle.
		link, err := io.ReadAll(io.LimitReader(rc, 4096))
		if err != nil {
			return err
		}
		if filepath.IsAbs(string(link)) || strings.Contains(string(link), "..") {
			return fmt.Errorf("unsafe link in update: %s", f.Name)
		}
		return os.Symlink(string(link), target)
	}
	mode := f.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
