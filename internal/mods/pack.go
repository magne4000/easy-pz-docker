package mods

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	"github.com/magne4000/easy-pz-docker/internal/publicapi"
)

type Pack struct {
	Path   string `json:"-"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	// Files is the path of the pack's publicapi.PackFiles listing.
	Files string `json:"-"`
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

func (p *Packer) paths(key string) (zipPath, sumPath, filesPath string) {
	z := filepath.Join(p.dir, "pack-"+key+".zip")
	return z, z + ".sha256", filepath.Join(p.dir, "pack-"+key+".files.json")
}

// Get returns the pack when ready; otherwise it starts a background build
// (at most one per key) and reports ready=false. A previous build failure is
// returned as an error once.
func (p *Packer) Get(key string, ms []ModInfo) (Pack, bool, error) {
	zp, sp, fp := p.paths(key)
	// Packs cached before listings existed are rebuilt (build removes them).
	if st, err := os.Stat(zp); err == nil && fileExists(fp) {
		sum, _ := os.ReadFile(sp)
		return Pack{Path: zp, Size: st.Size(), SHA256: strings.TrimSpace(string(sum)), Files: fp}, true, nil
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
		err := p.build(zp, sp, fp, cp)
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

// build writes the listing and checksum before the zip: the zip's presence
// means the pack is complete. A zip left without a listing goes first, so it
// does not count as ready once the new listing exists.
func (p *Packer) build(zp, sp, fp string, ms []ModInfo) error {
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return err
	}
	if err := os.Remove(zp); err != nil && !errors.Is(err, fs.ErrNotExist) {
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
	var files []publicapi.PackFile
	for _, m := range ms {
		if seen[m.FolderName] {
			continue
		}
		seen[m.FolderName] = true
		added, err := addTree(zw, m.Dir, m.FolderName)
		if err != nil {
			return fmt.Errorf("%s: %w", m.FolderName, err)
		}
		files = append(files, added...)
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := packedSizes(tmp.File, files); err != nil {
		return err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	listing, err := json.Marshal(publicapi.PackFiles{SHA256: sum, Files: files})
	if err != nil {
		return err
	}
	if err := renameio.WriteFile(fp, listing, 0o644, renameio.WithTempDir(p.dir)); err != nil {
		return err
	}
	if err := os.WriteFile(sp, []byte(sum+"\n"), 0o644); err != nil {
		return err
	}
	return tmp.CloseAtomicallyReplace()
}

// packedSizes reads each entry's compressed size back from the written zip.
func packedSizes(f *os.File, files []publicapi.PackFile) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		return err
	}
	packed := make(map[string]int64, len(zr.File))
	for _, e := range zr.File {
		packed[e.Name] = int64(e.CompressedSize64)
	}
	for i := range files {
		files[i].Packed = packed[files[i].Path]
	}
	return nil
}

func addTree(zw *zip.Writer, dir, prefix string) ([]publicapi.PackFile, error) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	sort.Strings(files)
	out := make([]publicapi.PackFile, 0, len(files))
	for _, f := range files {
		rel, err := filepath.Rel(real, f)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(f)
		if err != nil {
			return nil, err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return nil, err
		}
		hdr.Name = prefix + "/" + filepath.ToSlash(rel)
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, err
		}
		src, err := os.Open(f)
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(w, h), src)
		src.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, publicapi.PackFile{Path: hdr.Name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))})
	}
	return out, nil
}

// Prune deletes cached packs whose key is not in keep.
func (p *Packer) Prune(keep ...string) {
	want := map[string]bool{}
	for _, k := range keep {
		want[k] = true
	}
	entries, err := os.ReadDir(p.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	for _, e := range entries {
		key, _, _ := strings.Cut(strings.TrimPrefix(e.Name(), "pack-"), ".")
		if strings.HasPrefix(e.Name(), "pack-") && !want[key] {
			os.Remove(filepath.Join(p.dir, e.Name()))
		}
	}
}
