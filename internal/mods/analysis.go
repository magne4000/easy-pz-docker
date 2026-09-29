package mods

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SortLoadOrder is a stable topological sort: requirements come before the
// mods needing them, otherwise input order is preserved.
func SortLoadOrder(in []ModInfo) ([]ModInfo, map[string][]string, [][]string) {
	idx := map[string]int{}
	for i, m := range in {
		if _, dup := idx[m.ID]; !dup {
			idx[m.ID] = i
		}
	}
	missing := map[string][]string{}
	for _, m := range in {
		for _, r := range m.Requires {
			if _, ok := idx[r]; !ok {
				missing[m.ID] = append(missing[m.ID], r)
			}
		}
	}
	const (
		unvisited = iota
		visiting
		done
	)
	state := make([]int, len(in))
	out := make([]ModInfo, 0, len(in))
	var cycles [][]string
	var stack []string
	var visit func(i int)
	visit = func(i int) {
		switch state[i] {
		case done:
			return
		case visiting:
			id := in[i].ID
			for k := len(stack) - 1; k >= 0; k-- {
				if stack[k] == id {
					cycles = append(cycles, append([]string(nil), stack[k:]...))
					break
				}
			}
			return
		}
		state[i] = visiting
		stack = append(stack, in[i].ID)
		for _, r := range in[i].Requires {
			if j, ok := idx[r]; ok && j != i {
				visit(j)
			}
		}
		stack = stack[:len(stack)-1]
		state[i] = done
		out = append(out, in[i])
	}
	for i := range in {
		visit(i)
	}
	return out, missing, cycles
}

type Conflict struct {
	Path string   `json:"path"`
	Mods []string `json:"mods"`
}

var ignoredFiles = map[string]bool{"mod.info": true, "poster.png": true, "preview.png": true, "icon.png": true}

func indexMod(m ModInfo, into map[string][]string) error {
	roots := m.MediaDirs
	if len(roots) == 0 {
		roots = []string{m.Dir}
	}
	seen := map[string]bool{}
	for _, root := range roots {
		real, err := filepath.EvalSymlinks(root)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		media := filepath.Join(real, "media")
		err = filepath.WalkDir(media, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if !d.Type().IsRegular() || ignoredFiles[strings.ToLower(d.Name())] {
				return nil
			}
			rel, err := filepath.Rel(real, p)
			if err != nil || strings.HasPrefix(rel, "..") {
				return nil
			}
			key := strings.ToLower(filepath.ToSlash(rel))
			if !seen[key] {
				seen[key] = true
				into[key] = append(into[key], m.ID)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("index %s: %w", m.ID, err)
		}
	}
	return nil
}

// FindConflicts lists media files provided by more than one mod.
func FindConflicts(mods []ModInfo) ([]Conflict, error) {
	idx := map[string][]string{}
	for _, m := range mods {
		if err := indexMod(m, idx); err != nil {
			return nil, err
		}
	}
	out := []Conflict{}
	for p, ids := range idx {
		if len(ids) > 1 {
			out = append(out, Conflict{Path: p, Mods: ids})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

type LinkResult struct {
	Created []string `json:"created"`
	Updated []string `json:"updated"`
	Removed []string `json:"removed"`
	Kept    []string `json:"kept"`
	Errors  []string `json:"errors"`
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

// mirrorMarker, inside a mods-dir entry, marks it as a pzman mirror and holds
// its source dir. PZ ignores it: it is neither common/ nor a version folder.
const mirrorMarker = ".pzman-mirror"

// ReconcileLinks makes modsDir hold exactly the desired mods as mirrors:
// real directories whose files are symlinks into the workshop download. A
// directory symlink does not load: PZ's script loader relativizes files
// against the canonical version dir while walking the link path, and its Lua
// loader canonicalizes every directory it walks, so only leaf files may be
// links. Only what pzman owns is removed — mirrors, and the directory symlinks
// into contentRoot that earlier versions created; user-placed mods and
// other links are left alone.
func ReconcileLinks(modsDir, contentRoot string, desired map[string]string) (LinkResult, error) {
	res := LinkResult{Created: []string{}, Updated: []string{}, Removed: []string{}, Kept: []string{}, Errors: []string{}}
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		return res, err
	}
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return res, err
	}
	existing := map[string]fs.DirEntry{}
	for _, e := range entries {
		existing[e.Name()] = e
		if _, want := desired[e.Name()]; want {
			continue
		}
		p := filepath.Join(modsDir, e.Name())
		if !owned(p, e, contentRoot) {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", e.Name(), err))
			continue
		}
		res.Removed = append(res.Removed, e.Name())
	}
	names := make([]string, 0, len(desired))
	for n := range desired {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		p := filepath.Join(modsDir, name)
		e, ok := existing[name]
		switch {
		case ok && e.Type()&fs.ModeSymlink != 0:
			// the pre-mirror directory link, or a link under a name pzman now manages
			if err := os.Remove(p); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", name, err))
				continue
			}
		case ok && !isMirror(p):
			res.Errors = append(res.Errors, fmt.Sprintf("%s: a real file or directory with this name already exists in %s; not replacing it", name, modsDir))
			continue
		}
		changed, err := syncMirror(p, desired[name])
		switch {
		case err != nil:
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", name, err))
		case !ok:
			res.Created = append(res.Created, name)
		case changed:
			res.Updated = append(res.Updated, name)
		default:
			res.Kept = append(res.Kept, name)
		}
	}
	return res, nil
}

func isMirror(p string) bool {
	st, err := os.Lstat(filepath.Join(p, mirrorMarker))
	return err == nil && st.Mode().IsRegular()
}

func owned(p string, e fs.DirEntry, contentRoot string) bool {
	if e.Type()&fs.ModeSymlink == 0 {
		return e.IsDir() && isMirror(p)
	}
	target, err := os.Readlink(p)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(p), target)
	}
	return within(target, contentRoot)
}

// syncMirror makes dst a mirror of src — the same directories, one symlink per
// file pointing at the source file — and reports whether anything changed.
func syncMirror(dst, src string) (bool, error) {
	changed := false
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return false, err
	}
	marker := filepath.Join(dst, mirrorMarker)
	if b, err := os.ReadFile(marker); err != nil || string(b) != src+"\n" {
		if err := os.WriteFile(marker, []byte(src+"\n"), 0o644); err != nil {
			return false, err
		}
		changed = true
	}
	want := map[string]bool{mirrorMarker: true}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil || rel == "." {
			return err
		}
		want[rel] = true
		target := filepath.Join(dst, rel)
		cur, lerr := os.Lstat(target)
		if d.IsDir() {
			if lerr == nil && cur.IsDir() {
				return nil
			}
			if lerr == nil {
				if err := os.RemoveAll(target); err != nil {
					return err
				}
			}
			changed = true
			return os.Mkdir(target, 0o755)
		}
		if lerr == nil && cur.Mode()&fs.ModeSymlink != 0 {
			if t, _ := os.Readlink(target); t == p {
				return nil
			}
		}
		if lerr == nil {
			if err := os.RemoveAll(target); err != nil {
				return err
			}
		}
		changed = true
		return os.Symlink(p, target)
	})
	if err != nil {
		return changed, err
	}
	// drop what the source no longer has
	err = filepath.WalkDir(dst, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dst, p)
		if err != nil || rel == "." || want[rel] {
			return err
		}
		changed = true
		if err := os.RemoveAll(p); err != nil {
			return err
		}
		if d.IsDir() {
			return fs.SkipDir
		}
		return nil
	})
	return changed, err
}

// VerifyLinks is the boot self-check: every enabled mod must be a
// directory in the mods dir whose file links all resolve, holding a mod.info
// where PZ looks for it.
func VerifyLinks(modsDir string, names []string) error {
	var errs []error
	for _, name := range names {
		p := filepath.Join(modsDir, name)
		st, err := os.Lstat(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("mod %s: missing from %s: %w", name, modsDir, err))
			continue
		}
		if !st.IsDir() {
			errs = append(errs, fmt.Errorf("mod %s: %s is not a directory", name, p))
			continue
		}
		broken, first := 0, ""
		err = filepath.WalkDir(p, func(q string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&fs.ModeSymlink != 0 {
				if _, err := os.Stat(q); err != nil {
					if broken == 0 {
						first = q
					}
					broken++
				}
			}
			return nil
		})
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("mod %s: not readable: %w", name, err))
		case broken > 0:
			errs = append(errs, fmt.Errorf("mod %s: %d file link(s) do not resolve, e.g. %s", name, broken, first))
		default:
			if _, ok := findInfoDir(p); !ok {
				errs = append(errs, fmt.Errorf("mod %s: no mod.info where PZ looks for it (a version folder or common/)", name))
			}
		}
	}
	return errors.Join(errs...)
}
