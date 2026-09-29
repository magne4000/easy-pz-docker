package mods

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/renameio/v2"
	"golang.org/x/sync/singleflight"
)

type Pack struct {
	Path   string `json:"-"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Packer builds client mod zips (non-Steam layout: each <ModName> at the
// archive root) and caches them on disk by content key.
type Packer struct {
	dir    string
	log    *slog.Logger
	builds singleflight.Group
	mu     sync.Mutex
	failed map[string]error
}

func NewPacker(cacheDir string, log *slog.Logger) *Packer {
	return &Packer{dir: cacheDir, log: log, failed: map[string]error{}}
}

// PackKey identifies a mod set: folder names plus each item's version.
func PackKey(ms []ModInfo, updated map[string]time.Time) string {
	h := sha256.New()
	for _, m := range ms {
		fmt.Fprintf(h, "%s\x00%s\x00%d\n", m.WorkshopID, m.FolderName, updated[m.WorkshopID].Unix())
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

func (p *Packer) paths(key string) (zipPath, sumPath string) {
	z := filepath.Join(p.dir, "pack-"+key+".zip")
	return z, z + ".sha256"
}

// Get returns the pack when ready; otherwise it starts a background build
// (at most one per key) and reports ready=false. A previous build failure is
// returned as an error once.
func (p *Packer) Get(key string, ms []ModInfo) (Pack, bool, error) {
	zp, sp := p.paths(key)
	if st, err := os.Stat(zp); err == nil {
		sum, _ := os.ReadFile(sp)
		return Pack{Path: zp, Size: st.Size(), SHA256: strings.TrimSpace(string(sum))}, true, nil
	}
	p.mu.Lock()
	err, failed := p.failed[key]
	delete(p.failed, key)
	p.mu.Unlock()
	if failed {
		return Pack{}, false, err
	}
	cp := append([]ModInfo(nil), ms...)
	p.builds.DoChan(key, func() (any, error) {
		start := time.Now()
		err := p.build(zp, sp, cp)
		if err != nil {
			p.mu.Lock()
			p.failed[key] = err
			p.mu.Unlock()
			p.log.Error("mod pack build failed", "key", key, "err", err)
		} else {
			p.log.Info("mod pack built", "key", key, "mods", len(cp), "took", time.Since(start).Round(time.Millisecond))
		}
		return nil, err
	})
	return Pack{}, false, nil
}

func (p *Packer) build(zp, sp string, ms []ModInfo) error {
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return err
	}
	tmp, err := renameio.NewPendingFile(zp, renameio.WithTempDir(p.dir))
	if err != nil {
		return err
	}
	defer tmp.Cleanup()
	h := sha256.New()
	zw := zip.NewWriter(io.MultiWriter(tmp, h))
	seen := map[string]bool{}
	for _, m := range ms {
		if seen[m.FolderName] {
			continue
		}
		seen[m.FolderName] = true
		if err := addTree(zw, m.Dir, m.FolderName); err != nil {
			return fmt.Errorf("%s: %w", m.FolderName, err)
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := os.WriteFile(sp, []byte(hex.EncodeToString(h.Sum(nil))+"\n"), 0o644); err != nil {
		return err
	}
	return tmp.CloseAtomicallyReplace()
}

func addTree(zw *zip.Writer, dir, prefix string) error {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	var files []string
	err = filepath.WalkDir(real, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		rel, err := filepath.Rel(real, f)
		if err != nil {
			return err
		}
		info, err := os.Stat(f)
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = prefix + "/" + filepath.ToSlash(rel)
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		src, err := os.Open(f)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, src)
		src.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// Prune deletes cached packs whose key is not in keep.
func (p *Packer) Prune(keep ...string) {
	want := map[string]bool{}
	for _, k := range keep {
		want["pack-"+k+".zip"] = true
		want["pack-"+k+".zip.sha256"] = true
	}
	entries, err := os.ReadDir(p.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "pack-") && !want[e.Name()] {
			os.Remove(filepath.Join(p.dir, e.Name()))
		}
	}
}
