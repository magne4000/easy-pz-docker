package mods

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type ModInfo struct {
	ID, Name, Description, Poster, ModVersion, VersionMin string
	Requires                                              []string
	WorkshopID                                            string
	FolderName                                            string
	Dir                                                   string
	// MediaDirs are the dirs whose media/ PZ loads for this mod, later ones
	// overriding earlier ones: common then the version dir (B42), or the mod
	// folder itself (B41 layout).
	MediaDirs []string
	Maps      []string
}

var ErrNoID = errors.New("mod.info has no id=")

func ParseModInfo(r io.Reader) (ModInfo, error) {
	var m ModInfo
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch k {
		case "id":
			m.ID = strings.TrimPrefix(v, "\\")
		case "name":
			m.Name = v
		case "description":
			if m.Description != "" {
				m.Description += "\n"
			}
			m.Description += v
		case "poster":
			if m.Poster == "" {
				m.Poster = v
			}
		case "modversion":
			m.ModVersion = v
		case "versionmin":
			m.VersionMin = v
		case "require":
			for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' }) {
				p = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(p), "\\"))
				if p != "" {
					m.Requires = append(m.Requires, p)
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return m, err
	}
	if m.ID == "" {
		return m, ErrNoID
	}
	return m, nil
}

// minVersionScore is B42's floor for version folders
// (ChooseGameInfo.getMinRequiredVersion = 42.0).
const minVersionScore = 42000

// versionScore mirrors ZomboidFileSystem.getGameVersionIntFromName:
// "42" → 42000, "42.15" → 42015 (minor capped at 999); unparsable parts count as 0.
func versionScore(name string) int {
	parts := strings.Split(name, ".")
	major, _ := strconv.Atoi(parts[0])
	if len(parts) == 1 {
		return major * 1000
	}
	minor, _ := strconv.Atoi(parts[1])
	return major*1000 + min(minor, 999)
}

// versionDir mirrors ZomboidFileSystem.getModVersionDirName: the entry with
// the highest score ≥ 42.0, later entries winning ties. PZ also caps it at the
// running game version, which pzman cannot know before the first launch, so a
// folder for a newer game version is picked here while PZ would skip it.
func versionDir(dir string) string {
	entries, _ := os.ReadDir(dir)
	best, name := minVersionScore, ""
	for _, e := range entries {
		if v := versionScore(e.Name()); v >= best {
			best, name = v, e.Name()
		}
	}
	return name
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// findInfoDir returns the dir holding the mod.info PZ reads for a mod folder
// (ChooseGameInfo.readModInfoAux): <versionDir>/mod.info, else common/mod.info.
// A mod.info at the folder root is the B41 layout, which B42 no longer discovers.
func findInfoDir(dir string) (string, bool) {
	if v := versionDir(dir); v != "" && fileExists(filepath.Join(dir, v, "mod.info")) {
		return filepath.Join(dir, v), true
	}
	if fileExists(filepath.Join(dir, "common", "mod.info")) {
		return filepath.Join(dir, "common"), true
	}
	if fileExists(filepath.Join(dir, "mod.info")) {
		return dir, true
	}
	return "", false
}

// mediaDirs lists the dirs whose media/ PZ loads (ZomboidFileSystem.loadMod),
// in override order.
func mediaDirs(dir, infoDir string) []string {
	if infoDir == dir {
		return []string{dir}
	}
	out := []string{filepath.Join(dir, "common")}
	if v := versionDir(dir); v != "" {
		out = append(out, filepath.Join(dir, v))
	}
	return out
}

func listMaps(dirs ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		entries, err := os.ReadDir(filepath.Join(d, "media", "maps"))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
				continue
			}
			if !seen[e.Name()] {
				seen[e.Name()] = true
				out = append(out, e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

func ScanWorkshopItem(itemDir, wsid string) ([]ModInfo, error) {
	modsRoot := filepath.Join(itemDir, "mods")
	entries, err := os.ReadDir(modsRoot)
	if err != nil {
		return nil, fmt.Errorf("scan workshop item %s: %w", wsid, err)
	}
	var out []ModInfo
	for _, e := range entries {
		dir, err := filepath.Abs(filepath.Join(modsRoot, e.Name()))
		if err != nil {
			return nil, err
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		infoDir, ok := findInfoDir(dir)
		if !ok {
			continue
		}
		f, err := os.Open(filepath.Join(infoDir, "mod.info"))
		if err != nil {
			return nil, err
		}
		m, err := ParseModInfo(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("workshop item %s, mod %s: %w", wsid, e.Name(), err)
		}
		m.WorkshopID = wsid
		m.FolderName = e.Name()
		m.Dir = dir
		m.MediaDirs = mediaDirs(dir, infoDir)
		m.Maps = listMaps(m.MediaDirs...)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FolderName < out[j].FolderName })
	return out, nil
}
