package app

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/caarlos0/env/v11"
)

// EnvFileName is the settings file read from the executable's directory. The
// Pelican plugin writes it next to the binary (.easy-pz/pzman.env).
const EnvFileName = "pzman.env"

// Precedence, highest first, for every variable:
//
//  1. the environment (egg variables, set by Wings, always apply);
//  2. pzman.env;
//  3. Pelican mode only: the egg alias (eggAliases) or derived value (heap),
//     looked up in the environment then pzman.env;
//  4. Pelican mode only: the server-folder default (pelicanDefaults);
//  5. the envDefault tag.
//
// So an explicit pzman variable (DEFAULT_PORT) beats its egg alias
// (SERVER_PORT) wherever each one is set.

// eggAliases maps the stock PZ egg's variables onto pzman's (Pelican mode).
var eggAliases = []struct{ egg, key string }{
	{"SERVER_PORT", "DEFAULT_PORT"},
	{"STEAM_PORT", "UDP_PORT"},
	{"ADMIN_USER", "ADMIN_USERNAME"},
	{"SRCDS_BETAID", "SERVER_BRANCH"},
}

// pelicanDefaults are the server folder layout (relative to the root,
// /home/container), used when the variable is unset or empty.
var pelicanDefaults = []struct{ key, rel string }{
	{"PANEL_INSTALL_DIR", "."},
	{"PANEL_DATA_DIR", ".cache"},
	{"PANEL_DB_PATH", ".easy-pz/pzman.db"},
	{"PANEL_BACKUP_DIR", ".easy-pz/backups"},
	{"PANEL_CACHE_DIR", ".easy-pz/cache"},
	{"PANEL_STEAM_HOME", ".easy-pz/steam"},
	{"PANEL_LOG_DIR", ".easy-pz/logs"},
	{"PANEL_STEAMCMD", "steamcmd/steamcmd.sh"},
}

// pelicanRoot is the server folder: the binary is <root>/.easy-pz/pzman.
func pelicanRoot(exe string) string { return filepath.Dir(filepath.Dir(exe)) }

// HeapFromServerMemory is the default -Xmx (GiB) for a Wings memory limit
// (SERVER_MEMORY, MiB): 75 % of it, rounded down, at least 1. The other
// quarter covers the JVM's off-heap memory, SteamCMD and pzman, which all
// count against the same container limit.
func HeapFromServerMemory(mib int) int { return max(1, mib*3/(4*1024)) }

// sources records where each variable's value came from, for error messages.
type sources map[string]string

// gatherVars merges pzman.env and the environment (which wins), then applies
// the Pelican-mode aliases, heap and path defaults when PZMAN_HOST=pelican.
// With a nil map the error is fatal; otherwise it is one more problem to report.
func gatherVars(environ []string, exe string) (map[string]string, sources, error) {
	vars, src := map[string]string{}, sources{}
	file := filepath.Join(filepath.Dir(exe), EnvFileName)
	fileVars, err := readEnvFile(file)
	if err != nil {
		return nil, nil, err
	}
	for k, v := range fileVars {
		vars[k], src[k] = v, file
	}
	for k, v := range env.ToMap(environ) {
		vars[k], src[k] = v, "the environment"
	}
	if Host(vars["PZMAN_HOST"]) != HostPelican {
		return vars, src, nil
	}
	var errs []error
	for _, a := range eggAliases {
		if _, explicit := vars[a.key]; explicit {
			continue
		}
		if v := vars[a.egg]; v != "" {
			vars[a.key], src[a.key] = v, a.egg+" from "+src[a.egg]
		}
	}
	if _, explicit := vars["MEMORY_XMX_GB"]; !explicit {
		if s := vars["SERVER_MEMORY"]; s != "" {
			mib, err := strconv.Atoi(s)
			switch {
			case err != nil || mib < 0:
				errs = append(errs, fmt.Errorf("SERVER_MEMORY %q is not a number of MiB (from %s)", s, src["SERVER_MEMORY"]))
			case mib > 0: // 0: no limit, keep MEMORY_XMX_GB's default
				vars["MEMORY_XMX_GB"] = strconv.Itoa(HeapFromServerMemory(mib))
				src["MEMORY_XMX_GB"] = "derived from SERVER_MEMORY from " + src["SERVER_MEMORY"]
			}
		}
	}
	root := pelicanRoot(exe)
	for _, d := range pelicanDefaults {
		switch v := vars[d.key]; {
		case v == "":
			vars[d.key] = filepath.Join(root, d.rel)
		case !filepath.IsAbs(v): // e.g. ./steamcmd/steamcmd.sh: children run in other directories
			vars[d.key] = filepath.Join(root, v)
		}
	}
	if vars["STEAM_PLATFORM"] == "" {
		// The yolk image does not set it; steamcmd.sh then runs the 32-bit binary.
		vars["STEAM_PLATFORM"] = "linux64"
	}
	return vars, src, errors.Join(errs...)
}

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// readEnvFile parses KEY=value lines; # comments and blank lines are ignored,
// values are verbatim (no quotes, no expansion). A missing file is no error.
func readEnvFile(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if t := strings.TrimSpace(line); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || !envKey.MatchString(k) {
			return nil, fmt.Errorf("%s:%d: expected KEY=value", path, n)
		}
		out[k] = v
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}

// fieldKeys maps Config field names to their variable names.
var fieldKeys = func() map[string]string {
	out := map[string]string{}
	t := reflect.TypeFor[Config]()
	for i := range t.NumField() {
		if tag := t.Field(i).Tag.Get("env"); tag != "" {
			out[t.Field(i).Name], _, _ = strings.Cut(tag, ",")
		}
	}
	return out
}()

// annotate appends, to each error, the source of the variables it names.
func (s sources) annotate(err error) error {
	if err == nil {
		return nil
	}
	var leaves []error
	var walk func(error)
	walk = func(e error) {
		if j, ok := e.(interface{ Unwrap() []error }); ok {
			for _, c := range j.Unwrap() {
				walk(c)
			}
			return
		}
		if agg, ok := e.(env.AggregateError); ok {
			for _, c := range agg.Errors {
				walk(c)
			}
			return
		}
		leaves = append(leaves, e)
	}
	walk(err)
	out := make([]error, 0, len(leaves))
	for _, e := range leaves {
		var keys []string
		var pe env.ParseError
		if errors.As(e, &pe) {
			key := fieldKeys[pe.Name]
			keys = []string{key}
			e = fmt.Errorf("%s: %w", key, e)
		} else {
			keys = mentionedKeys(e.Error())
		}
		var from []string
		for _, k := range keys {
			if where, ok := s[k]; ok {
				from = append(from, k+" from "+where)
			}
		}
		if len(from) > 0 {
			e = fmt.Errorf("%w (%s)", e, strings.Join(from, "; "))
		}
		out = append(out, e)
	}
	return errors.Join(out...)
}

// mentionedKeys lists the Config variable names found as words in msg.
func mentionedKeys(msg string) []string {
	var out []string
	for _, k := range fieldKeys {
		for i := 0; ; {
			j := strings.Index(msg[i:], k)
			if j < 0 {
				break
			}
			start, end := i+j, i+j+len(k)
			if (start == 0 || !isKeyByte(msg[start-1])) && (end == len(msg) || !isKeyByte(msg[end])) {
				out = append(out, k)
				break
			}
			i = end
		}
	}
	sort.Strings(out)
	return out
}

func isKeyByte(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}
