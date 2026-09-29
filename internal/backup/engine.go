package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/renameio/v2"
	"github.com/klauspost/compress/zstd"
)

const manifestName = "manifest.json"

type Set struct {
	Root    string
	Include []string
}

// DefaultSet is the world, the account db, and the server's config files:
// restoring a world against a mismatched config is not a restore.
func DefaultSet(dataDir, n string) Set {
	return Set{Root: dataDir, Include: []string{
		"Saves/Multiplayer/" + n,
		"db/" + n + ".db", "db/" + n + ".db-journal", "db/" + n + ".db-wal", "db/" + n + ".db-shm",
		"Server/" + n + ".ini", "Server/" + n + "_SandboxVars.lua", "Server/" + n + "_spawnregions.lua",
	}}
}

type Fingerprint struct {
	NewestMtime time.Time `json:"newestMtime"`
	TotalSize   int64     `json:"totalSize"`
	FileCount   int       `json:"fileCount"`
}

func (f Fingerprint) String() string {
	return fmt.Sprintf("%d-%d-%d", f.NewestMtime.UnixNano(), f.TotalSize, f.FileCount)
}

type FileEntry struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	Mode    uint32    `json:"mode"`
	ModTime time.Time `json:"modTime"`
	SHA256  string    `json:"sha256"`
}

type ModRef struct {
	WorkshopID  string    `json:"workshopId"`
	ModIDs      []string  `json:"modIds"`
	TimeUpdated time.Time `json:"timeUpdated"`
}

type Manifest struct {
	Version     int         `json:"version"`
	CreatedAt   time.Time   `json:"createdAt"`
	ServerName  string      `json:"serverName"`
	BuildID     string      `json:"buildId"`
	Reason      string      `json:"reason"`
	Note        string      `json:"note"`
	Include     []string    `json:"include"`
	Mods        []ModRef    `json:"mods"`
	Fingerprint Fingerprint `json:"fingerprint"`
	Files       []FileEntry `json:"files"`
}

type Progress struct {
	DoneBytes  int64  `json:"doneBytes"`
	TotalBytes int64  `json:"totalBytes"`
	File       string `json:"file"`
}

type walked struct {
	rel  string
	info fs.FileInfo
}

// walk lists regular files of the set (Lstat only, symlinks skipped).
func walk(s Set) ([]walked, Fingerprint, error) {
	var out []walked
	var fp Fingerprint
	for _, inc := range s.Include {
		base := filepath.Join(s.Root, filepath.FromSlash(inc))
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			rel, err := filepath.Rel(s.Root, p)
			if err != nil {
				return err
			}
			out = append(out, walked{rel: filepath.ToSlash(rel), info: info})
			fp.FileCount++
			fp.TotalSize += info.Size()
			if mt := info.ModTime(); mt.After(fp.NewestMtime) {
				fp.NewestMtime = mt
			}
			return nil
		})
		if err != nil {
			return nil, fp, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	fp.NewestMtime = fp.NewestMtime.UTC()
	return out, fp, nil
}

// sameContent reports whether the walked files hold exactly the bytes the
// entries (a backup's manifest) recorded. Only files whose mtime moved are
// read: PZ's save rewrites files with identical bytes, which the
// mtime-based fingerprint alone cannot tell from a change.
func sameContent(ctx context.Context, root string, files []walked, entries []FileEntry) (bool, error) {
	if len(files) != len(entries) {
		return false, nil
	}
	byPath := make(map[string]FileEntry, len(entries))
	for _, e := range entries {
		byPath[e.Path] = e
	}
	for _, f := range files {
		e, ok := byPath[f.rel]
		if !ok || e.Size != f.info.Size() {
			return false, nil
		}
		if f.info.ModTime().Equal(e.ModTime) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		sum, err := hashFile(filepath.Join(root, filepath.FromSlash(f.rel)))
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if sum != e.SHA256 {
			return false, nil
		}
	}
	return true, nil
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func Create(ctx context.Context, s Set, dst string, m Manifest, onProgress func(Progress)) (Manifest, int64, error) {
	if onProgress == nil {
		onProgress = func(Progress) {}
	}
	files, fp, err := walk(s)
	if err != nil {
		return m, 0, fmt.Errorf("backup: scan: %w", err)
	}
	m.Version, m.Include, m.Fingerprint, m.Files = 1, append([]string(nil), s.Include...), fp, []FileEntry{}
	if m.Mods == nil {
		m.Mods = []ModRef{}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return m, 0, err
	}
	out, err := renameio.NewPendingFile(dst, renameio.WithTempDir(filepath.Dir(dst)), renameio.WithPermissions(0o666))
	if err != nil {
		return m, 0, err
	}
	defer out.Cleanup()
	zw, err := zstd.NewWriter(out, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return m, 0, err
	}
	tw := tar.NewWriter(zw)
	var done int64
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return m, 0, err
		}
		size := f.info.Size()
		hdr := &tar.Header{Name: f.rel, Mode: int64(f.info.Mode().Perm()), Size: size, ModTime: f.info.ModTime(), Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if err := tw.WriteHeader(hdr); err != nil {
			return m, 0, err
		}
		src, err := os.Open(filepath.Join(s.Root, filepath.FromSlash(f.rel)))
		var r io.Reader = zeroReader{} // vanished since the walk: keep the tar consistent
		if err == nil {
			r = io.MultiReader(src, zeroReader{}) // shrank since the walk: pad
		}
		h := sha256.New()
		_, cerr := io.CopyN(tw, io.TeeReader(r, h), size)
		if src != nil {
			src.Close()
		}
		if cerr != nil {
			return m, 0, fmt.Errorf("backup: %s: %w", f.rel, cerr)
		}
		done += size
		m.Files = append(m.Files, FileEntry{Path: f.rel, Size: size, Mode: uint32(f.info.Mode().Perm()), ModTime: f.info.ModTime().UTC(), SHA256: hex.EncodeToString(h.Sum(nil))})
		onProgress(Progress{DoneBytes: done, TotalBytes: fp.TotalSize, File: f.rel})
	}
	mj, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, 0, err
	}
	if err := tw.WriteHeader(&tar.Header{Name: manifestName, Mode: 0o644, Size: int64(len(mj)), ModTime: m.CreatedAt, Typeflag: tar.TypeReg}); err != nil {
		return m, 0, err
	}
	if _, err := tw.Write(mj); err != nil {
		return m, 0, err
	}
	if err := tw.Close(); err != nil {
		return m, 0, err
	}
	if err := zw.Close(); err != nil {
		return m, 0, err
	}
	st, err := out.Stat()
	if err != nil {
		return m, 0, err
	}
	if err := os.WriteFile(dst+".manifest.json", mj, 0o644); err != nil {
		return m, 0, err
	}
	if err := out.CloseAtomicallyReplace(); err != nil {
		os.Remove(dst + ".manifest.json")
		return m, 0, err
	}
	return m, st.Size(), nil
}

func openArchive(archive string) (*tar.Reader, func(), error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, nil, err
	}
	zr, err := zstd.NewReader(f)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return tar.NewReader(zr), func() { zr.Close(); f.Close() }, nil
}

func ReadManifest(archive string) (Manifest, error) {
	var m Manifest
	if b, err := os.ReadFile(archive + ".manifest.json"); err == nil {
		return m, json.Unmarshal(b, &m)
	}
	tr, closeFn, err := openArchive(archive)
	if err != nil {
		return m, err
	}
	defer closeFn()
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return m, errors.New("backup: archive has no manifest")
		}
		if err != nil {
			return m, err
		}
		if h.Name == manifestName {
			return m, json.NewDecoder(tr).Decode(&m)
		}
	}
}

func safeName(name string) (string, error) {
	c := path.Clean(name)
	if name == "" || path.IsAbs(name) || c == ".." || strings.HasPrefix(c, "../") || strings.Contains(name, `\`) {
		return "", fmt.Errorf("backup: unsafe path %q in archive", name)
	}
	return c, nil
}

// scan iterates the archive's files, verifying them against the manifest and
// handing each verified-in-progress stream to sink (nil: just hash).
func scan(ctx context.Context, archive string, m Manifest, sink func(e FileEntry, r io.Reader) error, onProgress func(Progress)) error {
	want := make(map[string]FileEntry, len(m.Files))
	var total int64
	for _, f := range m.Files {
		want[f.Path] = f
		total += f.Size
	}
	tr, closeFn, err := openArchive(archive)
	if err != nil {
		return err
	}
	defer closeFn()
	seen := map[string]bool{}
	var done int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("backup: read archive: %w", err)
		}
		if h.Name == manifestName {
			continue
		}
		name, err := safeName(h.Name)
		if err != nil {
			return err
		}
		e, ok := want[name]
		if !ok {
			return fmt.Errorf("backup: archive entry %q is not in the manifest", name)
		}
		if h.Typeflag != tar.TypeReg {
			return fmt.Errorf("backup: archive entry %q is not a regular file", name)
		}
		if h.Size != e.Size {
			return fmt.Errorf("backup: %s: size %d, manifest says %d", name, h.Size, e.Size)
		}
		hs := sha256.New()
		r := io.TeeReader(tr, hs)
		if sink != nil {
			err = sink(e, r)
		} else {
			_, err = io.Copy(io.Discard, r)
		}
		if err != nil {
			return err
		}
		if got := hex.EncodeToString(hs.Sum(nil)); got != e.SHA256 {
			return fmt.Errorf("backup: %s: checksum mismatch", name)
		}
		seen[name] = true
		done += e.Size
		if onProgress != nil {
			onProgress(Progress{DoneBytes: done, TotalBytes: total, File: name})
		}
	}
	for _, f := range m.Files {
		if !seen[f.Path] {
			return fmt.Errorf("backup: %s is missing from the archive", f.Path)
		}
	}
	return nil
}

func Verify(ctx context.Context, archive string) error {
	m, err := ReadManifest(archive)
	if err != nil {
		return err
	}
	return scan(ctx, archive, m, nil, nil)
}

type move struct{ from, to string }

// Restore is manifest-driven: extract to staging, verify everything,
// then swap each included path in, rolling back on failure.
func Restore(ctx context.Context, archive, root string, onProgress func(Progress)) error {
	m, err := ReadManifest(archive)
	if err != nil {
		return err
	}
	for _, inc := range m.Include {
		if _, err := safeName(inc); err != nil {
			return err
		}
	}
	ts := strconv.FormatInt(time.Now().UnixNano(), 10)
	staging := filepath.Join(root, ".pzman-restore-"+ts)
	old := filepath.Join(root, ".pzman-old-"+ts)
	defer os.RemoveAll(staging)
	err = scan(ctx, archive, m, func(e FileEntry, r io.Reader) error {
		p := filepath.Join(staging, filepath.FromSlash(e.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(e.Mode).Perm()|0o200)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, r); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		os.Chmod(p, fs.FileMode(e.Mode).Perm())
		return os.Chtimes(p, e.ModTime, e.ModTime)
	}, onProgress)
	if err != nil {
		return err
	}
	var done []move
	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			os.RemoveAll(done[i].from)
			os.Rename(done[i].to, done[i].from)
		}
	}
	mv := func(from, to string) error {
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
		done = append(done, move{from: from, to: to})
		return nil
	}
	for _, inc := range m.Include {
		cur := filepath.Join(root, filepath.FromSlash(inc))
		if _, err := os.Lstat(cur); err == nil {
			if err := mv(cur, filepath.Join(old, filepath.FromSlash(inc))); err != nil {
				rollback()
				return fmt.Errorf("backup: restore %s: %w", inc, err)
			}
		}
	}
	var placed []string
	for _, inc := range m.Include {
		src := filepath.Join(staging, filepath.FromSlash(inc))
		if _, err := os.Lstat(src); err != nil {
			continue
		}
		dst := filepath.Join(root, filepath.FromSlash(inc))
		err := os.MkdirAll(filepath.Dir(dst), 0o755)
		if err == nil {
			err = os.Rename(src, dst)
		}
		if err != nil {
			for _, p := range placed {
				os.RemoveAll(p)
			}
			rollback()
			return fmt.Errorf("backup: restore %s: %w", inc, err)
		}
		placed = append(placed, dst)
	}
	os.RemoveAll(old)
	return nil
}

func Delete(archive string) error {
	var errs []error
	for _, p := range []string{archive, archive + ".manifest.json"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
