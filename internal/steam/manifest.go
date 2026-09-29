package steam

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	AppID         = "380870" // PZ dedicated server
	WorkshopAppID = "108600" // PZ game (workshop)
)

type AppManifest struct {
	BuildID     string
	Branch      string
	StateFlags  int
	LastUpdated time.Time
	SizeOnDisk  int64
}

type WorkshopItemState struct {
	ID          string
	Size        int64
	TimeUpdated time.Time
	Manifest    string
}

func parseFile(path string) (KV, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	root, err := ParseVDF(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return root, nil
}

func unixField(s string) time.Time {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n == 0 {
		return time.Time{}
	}
	return time.Unix(n, 0).UTC()
}

func ReadAppManifest(installDir string) (AppManifest, error) {
	root, err := parseFile(filepath.Join(installDir, "steamapps", "appmanifest_"+AppID+".acf"))
	if err != nil {
		return AppManifest{}, fmt.Errorf("read app manifest: %w", err)
	}
	st := root.Get("AppState")
	if st == nil {
		return AppManifest{}, errors.New("read app manifest: no AppState block")
	}
	m := AppManifest{
		BuildID:     st.String("buildid"),
		LastUpdated: unixField(st.String("LastUpdated")),
	}
	m.StateFlags, _ = strconv.Atoi(st.String("StateFlags"))
	m.SizeOnDisk, _ = strconv.ParseInt(st.String("SizeOnDisk"), 10, 64)
	m.Branch = st.String("UserConfig", "BetaKey")
	if m.Branch == "" {
		m.Branch = st.String("MountedConfig", "BetaKey")
	}
	if m.Branch == "public" {
		m.Branch = ""
	}
	return m, nil
}

func ReadWorkshopManifest(installDir string) (map[string]WorkshopItemState, error) {
	out := map[string]WorkshopItemState{}
	root, err := parseFile(filepath.Join(installDir, "steamapps", "workshop", "appworkshop_"+WorkshopAppID+".acf"))
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read workshop manifest: %w", err)
	}
	installed := root.Get("AppWorkshop", "WorkshopItemsInstalled")
	for id := range installed {
		it := installed.Get(id)
		s := WorkshopItemState{ID: id, Manifest: it.String("manifest"), TimeUpdated: unixField(it.String("timeupdated"))}
		s.Size, _ = strconv.ParseInt(it.String("size"), 10, 64)
		out[id] = s
	}
	return out, nil
}

func WorkshopContentRoot(installDir string) string {
	return filepath.Join(installDir, "steamapps", "workshop", "content", WorkshopAppID)
}

func WorkshopContentDir(installDir, id string) string {
	return filepath.Join(WorkshopContentRoot(installDir), id)
}

// ParseAppInfoBuildID extracts depots.branches.<branch>.buildid from noisy
// `app_info_print 380870` output.
func ParseAppInfoBuildID(output, branch string) (string, error) {
	if branch == "" {
		branch = "public"
	}
	start := strings.Index(output, `"`+AppID+`"`)
	if start < 0 {
		return "", errors.New("app_info_print: no app block in output")
	}
	body := output[start:]
	open := strings.IndexByte(body, '{')
	if open < 0 {
		return "", errors.New("app_info_print: malformed app block")
	}
	depth, end := 0, -1
	inStr := false
	for i := open; i < len(body); i++ {
		switch c := body[i]; {
		case c == '\\' && inStr:
			i++
		case c == '"':
			inStr = !inStr
		case c == '{' && !inStr:
			depth++
		case c == '}' && !inStr:
			depth--
			if depth == 0 {
				end = i + 1
			}
		}
		if end > 0 {
			break
		}
	}
	if end < 0 {
		return "", errors.New("app_info_print: truncated app block")
	}
	root, err := ParseVDF(strings.NewReader(body[:end]))
	if err != nil {
		return "", fmt.Errorf("app_info_print: %w", err)
	}
	id := root.String(AppID, "depots", "branches", branch, "buildid")
	if id == "" {
		return "", fmt.Errorf("app_info_print: branch %q has no buildid", branch)
	}
	return id, nil
}
