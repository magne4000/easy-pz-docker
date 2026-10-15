package game

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/andygrunwald/vdf"

	"github.com/magne4000/easy-pz-docker/internal/pz/gamever"
)

const steamFolder = "ProjectZomboid"

const (
	gogFolder    = "Project Zomboid"
	gogProductID = "1453298883"
)

const macBundle = "Project Zomboid.app"

type Install struct {
	Dir  string `json:"dir"`
	goos string
}

var ErrInvalid = errors.New("this folder does not contain Project Zomboid")

func Open(dir string) (Install, error) { return open(dir, runtime.GOOS) }

func open(dir, goos string) (Install, error) {
	dir = filepath.Clean(dir)
	// Linux installs keep the game in a projectzomboid/ subfolder next to the
	// launch script; accept that subfolder too.
	for _, d := range []string{dir, filepath.Dir(dir)} {
		in := Install{Dir: d, goos: goos}
		if in.Jar() != "" && fileExists(in.executable()) {
			return in, nil
		}
	}
	return Install{}, ErrInvalid
}

func (in Install) Jar() string { return firstFile(in.jarCandidates()) }

func (in Install) jarCandidates() []string {
	if in.goos == "darwin" {
		return []string{filepath.Join(in.Dir, macBundle, "Contents", "Java", "projectzomboid.jar")}
	}
	return []string{
		filepath.Join(in.Dir, "projectzomboid.jar"),
		filepath.Join(in.Dir, "projectzomboid", "projectzomboid.jar"),
		filepath.Join(in.Dir, "java", "projectzomboid.jar"),
	}
}

// steamAPI is Steam's library shipped with the game ("" when missing); see
// track.
func (in Install) steamAPI() string {
	switch in.goos {
	case "windows":
		// Unverified: 42.12 loaded its natives from win64/, Linux has since
		// moved them to natives/.
		return firstFile([]string{
			filepath.Join(in.Dir, "steam_api64.dll"),
			filepath.Join(in.Dir, "natives", "steam_api64.dll"),
			filepath.Join(in.Dir, "win64", "steam_api64.dll"),
		})
	case "darwin":
		return firstFile([]string{filepath.Join(in.Dir, macBundle, "Contents", "MacOS", "libsteam_api.dylib")})
	default:
		return firstFile([]string{filepath.Join(in.Dir, "projectzomboid", "natives", "libsteam_api.so")})
	}
}

func (in Install) executable() string {
	switch in.goos {
	case "windows":
		return filepath.Join(in.Dir, "ProjectZomboid64.exe")
	case "darwin":
		return filepath.Join(in.Dir, macBundle, "Contents", "Info.plist")
	default:
		return filepath.Join(in.Dir, "projectzomboid.sh")
	}
}

func (in Install) Version() string {
	v, err := gamever.FromJar(in.Jar())
	if err != nil {
		return ""
	}
	return v.String()
}

func Args() []string { return []string{"-nosteam"} }

func (in Install) Command() *exec.Cmd {
	var cmd *exec.Cmd
	switch in.goos {
	case "darwin":
		// If the game already runs, open only focuses it. -W returns when it
		// quits, so the Steam presence process sees the game's lifetime.
		cmd = exec.Command("open", append([]string{"-W", filepath.Join(in.Dir, macBundle), "--args"}, Args()...)...)
	default:
		cmd = exec.Command(in.executable(), Args()...)
	}
	cmd.Dir = in.Dir
	detach(cmd)
	return cmd
}

// Start launches the game, then its Steam presence (see startPresence).
func (in Install) Start() error {
	cmd := in.Command()
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	// Reaped, so that the presence process sees the game exit.
	go func() { _ = cmd.Wait() }()
	if lib := in.steamAPI(); lib != "" {
		startPresence(lib, pid)
	}
	return nil
}

func Detect() (Install, bool) {
	home, _ := os.UserHomeDir()
	for _, dir := range candidates(runtime.GOOS, home, os.Getenv) {
		if in, err := Open(dir); err == nil {
			return in, true
		}
	}
	return Install{}, false
}

func candidates(goos, home string, getenv func(string) string) []string {
	var steamRoots, extra []string
	switch goos {
	case "windows":
		extra = gogInstallDirs()
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
			if pf := getenv(env); pf != "" {
				steamRoots = append(steamRoots, filepath.Join(pf, "Steam"))
				extra = append(extra, filepath.Join(pf, steamFolder), filepath.Join(pf, gogFolder),
					filepath.Join(pf, "GOG Galaxy", "Games", gogFolder))
			}
		}
		if drive := getenv("SystemDrive"); drive != "" {
			extra = append(extra, filepath.Join(drive+`\`, "GOG Games", gogFolder))
		}
	case "darwin":
		steamRoots = []string{filepath.Join(home, "Library", "Application Support", "Steam")}
		extra = []string{"/Applications"}
	default:
		steamRoots = []string{
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".local", "share", "Steam"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
		}
		extra = []string{filepath.Join(home, "ProjectZomboid"), filepath.Join(home, "games", "ProjectZomboid")}
	}
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, root := range steamRoots {
		for _, lib := range steamLibraries(root) {
			add(filepath.Join(lib, "steamapps", "common", steamFolder))
		}
	}
	for _, p := range extra {
		add(p)
	}
	return out
}

func steamLibraries(root string) []string {
	libs := []string{root}
	f, err := os.Open(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return libs
	}
	defer f.Close()
	kv, err := vdf.NewParser(f).Parse()
	if err != nil {
		return libs
	}
	folders, _ := kv["libraryfolders"].(map[string]any)
	for _, v := range folders {
		entry, _ := v.(map[string]any)
		if p, _ := entry["path"].(string); p != "" {
			libs = append(libs, p)
		}
	}
	sort.Strings(libs[1:])
	return libs
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func firstFile(paths []string) string {
	for _, p := range paths {
		if fileExists(p) {
			return p
		}
	}
	return ""
}
