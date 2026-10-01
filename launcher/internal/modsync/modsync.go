package modsync

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/magne4000/easy-pz-docker/internal/publicapi"
	"github.com/magne4000/easy-pz-docker/launcher/internal/pzclient"
)

type Installed map[string]Item

type Item struct {
	SHA256  string   `json:"sha256"`
	Folders []string `json:"folders"`
}

func (in Installed) owns(folder string) bool {
	for _, it := range in {
		for _, f := range it.Folders {
			if strings.EqualFold(f, folder) {
				return true
			}
		}
	}
	return false
}

type Plan struct {
	Download []publicapi.PublicItem `json:"download"`
	Bytes    int64                  `json:"bytes"`
	UpToDate int                    `json:"upToDate"`
	// Foreign: existing folders the launcher did not install.
	Foreign []string `json:"foreign"`
}

func itemFolders(it publicapi.PublicItem) []string {
	var out []string
	for _, m := range it.Mods {
		if m.Folder != "" {
			out = append(out, m.Folder)
		}
	}
	return out
}

func MakePlan(modsDir string, data *publicapi.PublicData, in Installed) Plan {
	p := Plan{Download: []publicapi.PublicItem{}, Foreign: []string{}}
	for _, it := range data.Items {
		cur, ok := in[it.WorkshopID]
		if ok && cur.SHA256 != "" && strings.EqualFold(cur.SHA256, it.Download.SHA256) && foldersExist(modsDir, cur.Folders) {
			p.UpToDate++
			continue
		}
		p.Download = append(p.Download, it)
		p.Bytes += it.Download.Size
		for _, f := range itemFolders(it) {
			if dirExists(filepath.Join(modsDir, f)) && !in.owns(f) {
				p.Foreign = append(p.Foreign, f)
			}
		}
	}
	sort.Strings(p.Foreign)
	return p
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func foldersExist(modsDir string, folders []string) bool {
	if len(folders) == 0 {
		return false
	}
	for _, f := range folders {
		if !dirExists(filepath.Join(modsDir, f)) {
			return false
		}
	}
	return true
}

type Downloader interface {
	DownloadFile(ctx context.Context, dir, page, packURL, want string, progress func(int64)) (string, error)
}

type Progress struct {
	Index int    `json:"index"`
	Total int    `json:"total"`
	Title string `json:"title"`
	Bytes int64  `json:"bytes"`
	Size  int64  `json:"size"`
}

var ErrForeign = errors.New("some mod folders were not installed by the launcher")

type NotReadyError struct{ Title string }

func (e *NotReadyError) Error() string {
	return fmt.Sprintf("the server is still preparing %q, try again in a minute", e.Title)
}

type Syncer struct {
	ModsDir  string
	Page     string
	DL       Downloader
	Retry    time.Duration
	Attempts int
}

// save runs after each item so an interrupted sync keeps what it finished.
func (s *Syncer) Apply(ctx context.Context, p Plan, in Installed, takeOver bool, progress func(Progress), save func(Installed) error) error {
	if len(p.Foreign) > 0 && !takeOver {
		return ErrForeign
	}
	if err := os.MkdirAll(s.ModsDir, 0o755); err != nil {
		return err
	}
	for i, it := range p.Download {
		report := func(n int64) {
			if progress != nil {
				progress(Progress{Index: i, Total: len(p.Download), Title: it.Title, Bytes: n, Size: it.Download.Size})
			}
		}
		report(0)
		folders, err := s.installItem(ctx, it, report)
		if err != nil {
			return fmt.Errorf("%s: %w", it.Title, err)
		}
		in[it.WorkshopID] = Item{SHA256: it.Download.SHA256, Folders: folders}
		if err := save(in); err != nil {
			return err
		}
	}
	return nil
}

func (s *Syncer) installItem(ctx context.Context, it publicapi.PublicItem, report func(int64)) ([]string, error) {
	attempts := max(s.Attempts, 1)
	var zipPath string
	var err error
	for a := 0; a < attempts; a++ {
		zipPath, err = s.DL.DownloadFile(ctx, s.ModsDir, s.Page, it.Download.URL, it.Download.SHA256, report)
		if err == nil || !isNotReady(err) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.Retry):
		}
	}
	if isNotReady(err) {
		return nil, &NotReadyError{Title: it.Title}
	}
	if err != nil {
		return nil, err
	}
	defer os.Remove(zipPath)

	tmp, err := os.MkdirTemp(s.ModsDir, ".easypz-extract-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := extract(zipPath, tmp); err != nil {
		return nil, fmt.Errorf("unpack: %w", err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return nil, err
	}
	var folders []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := swapIn(filepath.Join(tmp, e.Name()), filepath.Join(s.ModsDir, e.Name())); err != nil {
			return nil, err
		}
		folders = append(folders, e.Name())
	}
	if len(folders) == 0 {
		return nil, errors.New("the archive holds no mod folder")
	}
	return folders, nil
}

func isNotReady(err error) bool { return errors.Is(err, pzclient.ErrNotReady) }

// swapIn uses renames so a crash never leaves a half-written folder.
func swapIn(src, dst string) error {
	old := ""
	if dirExists(dst) {
		old = filepath.Join(filepath.Dir(dst), ".easypz-old-"+filepath.Base(dst)+fmt.Sprint(time.Now().UnixNano()))
		if err := os.Rename(dst, old); err != nil {
			return fmt.Errorf("replace %s (is the game running?): %w", filepath.Base(dst), err)
		}
	}
	if err := os.Rename(src, dst); err != nil {
		if old != "" {
			os.Rename(old, dst)
		}
		return err
	}
	if old != "" {
		os.RemoveAll(old)
	}
	return nil
}

func extract(zipPath, dst string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	root, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		name := filepath.FromSlash(f.Name)
		target := filepath.Join(root, name)
		if !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe path in archive: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if !f.Mode().IsRegular() {
			continue // no symlinks from the network
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeEntry(f, target); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func Cleanup(modsDir string) {
	entries, _ := os.ReadDir(modsDir)
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".easypz-extract-") || strings.HasPrefix(n, ".easypz-old-") || strings.HasPrefix(n, ".easypz-download-") {
			os.RemoveAll(filepath.Join(modsDir, n))
		}
	}
}
