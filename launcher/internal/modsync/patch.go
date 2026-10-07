package modsync

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/magne4000/easy-pz-docker/internal/publicapi"
)

// errWholePack: the folders are missing or differ too much for a partial
// download to pay off. patchItem returns it with the listed pack's checksum,
// which data.json lacks while the pack is still being built.
var errWholePack = errors.New("download the whole pack")

// patchItem brings the item's folders on disk to the server's listing,
// downloading only the files whose content differs. It replaces files one by
// one, so an interrupted patch is not atomic; the item stays outdated in
// Installed until it completes, and the next sync compares again.
func (s *Syncer) patchItem(ctx context.Context, it publicapi.PublicItem, report func(n, size int64)) (Item, error) {
	var l *publicapi.PackFiles
	err := s.retry(ctx, it.Title, func() (err error) {
		l, err = s.DL.Files(ctx, s.Page, it.Files)
		return err
	})
	if err != nil {
		return Item{}, err
	}
	d, err := diffLocal(s.ModsDir, l)
	if err != nil {
		return Item{}, err
	}
	if len(d.folders) == 0 || d.packed*2 > d.total {
		return Item{SHA256: l.SHA256}, errWholePack
	}
	if len(d.need) > 0 {
		report(0, d.packed)
		var zipPath string
		err := s.retry(ctx, it.Title, func() (err error) {
			zipPath, err = s.DL.DownloadFiles(ctx, s.ModsDir, s.Page, it.Files, publicapi.FilesRequest{SHA256: l.SHA256, Files: d.need},
				func(n int64) { report(n, d.packed) })
			return err
		})
		if err != nil {
			return Item{}, err
		}
		defer os.Remove(zipPath)
		if err := placeFiles(zipPath, s.ModsDir, l, d.need); err != nil {
			return Item{}, fmt.Errorf("unpack: %w", err)
		}
	}
	for _, p := range d.extra {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Item{}, err
		}
	}
	// Deepest first; non-empty dirs stay. The whole pack has no empty dirs either.
	for _, dir := range slices.Backward(d.dirs) {
		os.Remove(dir)
	}
	return Item{SHA256: l.SHA256, Folders: d.folders}, nil
}

type localDiff struct {
	folders []string // the listing's mod folders, in order
	need    []int    // listing indexes whose local copy differs
	packed  int64    // compressed size of need
	total   int64    // compressed size of the whole listing
	extra   []string // local files the listing does not have
	dirs    []string // dirs under the folders, parents first
}

func diffLocal(modsDir string, l *publicapi.PackFiles) (localDiff, error) {
	var d localDiff
	listed := make(map[string]bool, len(l.Files))
	lower := make(map[string]string, len(l.Files))
	for i, f := range l.Files {
		folder, _, _ := strings.Cut(f.Path, "/")
		if !listingPath(f.Path) {
			return d, fmt.Errorf("unsafe path in the file listing: %s", f.Path)
		}
		if !slices.Contains(d.folders, folder) {
			d.folders = append(d.folders, folder)
		}
		listed[f.Path] = true
		lower[strings.ToLower(f.Path)] = f.Path
		d.total += f.Packed
		if !sameContent(filepath.Join(modsDir, filepath.FromSlash(f.Path)), f) {
			d.need = append(d.need, i)
			d.packed += f.Packed
		}
	}
	for _, folder := range d.folders {
		root := filepath.Join(modsDir, folder)
		err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if e.IsDir() {
				if p != root {
					d.dirs = append(d.dirs, p)
				}
				return nil
			}
			rel, err := filepath.Rel(modsDir, p)
			if err != nil {
				return err
			}
			key := filepath.ToSlash(rel)
			if listed[key] {
				return nil
			}
			// On a case-insensitive disk a name differing only in case is the
			// listed file itself, not an extra one.
			if want, ok := lower[strings.ToLower(key)]; ok && sameFile(p, filepath.Join(modsDir, filepath.FromSlash(want))) {
				return nil
			}
			d.extra = append(d.extra, p)
			return nil
		})
		if err != nil {
			return d, err
		}
	}
	return d, nil
}

// listingPath accepts "<folder>/<file...>" paths that stay inside the mods dir.
func listingPath(p string) bool {
	return strings.Contains(p, "/") && !strings.Contains(p, `\`) && filepath.IsLocal(filepath.FromSlash(p)) &&
		!slices.ContainsFunc(strings.Split(p, "/"), func(s string) bool { return s == "" || s == "." || s == ".." })
}

func sameContent(path string, f publicapi.PackFile) bool {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() != f.Size {
		return false
	}
	h, err := hashFile(path)
	return err == nil && strings.EqualFold(h, f.SHA256)
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sameFile(a, b string) bool {
	sa, err := os.Lstat(a)
	if err != nil {
		return false
	}
	sb, err := os.Lstat(b)
	return err == nil && os.SameFile(sa, sb)
}

// placeFiles checks every downloaded file against the listing before moving
// any into place. The staging dir sits in modsDir, so the moves are renames
// on one disk, and Cleanup sweeps it after a crash.
func placeFiles(zipPath, modsDir string, l *publicapi.PackFiles, need []int) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	byName := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		byName[f.Name] = f
	}
	stage, err := os.MkdirTemp(modsDir, ".easypz-extract-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, i := range need {
		want := l.Files[i]
		zf := byName[want.Path]
		if zf == nil {
			return fmt.Errorf("the server did not send %s", want.Path)
		}
		if err := writeChecked(zf, filepath.Join(stage, strconv.Itoa(i)), want); err != nil {
			return err
		}
	}
	for _, i := range need {
		target := filepath.Join(modsDir, filepath.FromSlash(l.Files[i].Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if st, err := os.Lstat(target); err == nil && st.IsDir() {
			if err := os.RemoveAll(target); err != nil {
				return err
			}
		}
		if err := os.Rename(filepath.Join(stage, strconv.Itoa(i)), target); err != nil {
			return fmt.Errorf("replace %s (is the game running?): %w", l.Files[i].Path, err)
		}
	}
	return nil
}

func writeChecked(zf *zip.File, target string, want publicapi.PackFile) error {
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(rc, want.Size+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != want.Size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want.SHA256) {
		return fmt.Errorf("%s is corrupted (checksum mismatch)", want.Path)
	}
	return nil
}
