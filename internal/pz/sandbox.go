package pz

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/renameio/v2"
)

// SandboxKind is the Lua type of a sandbox value, as found in the file.
type SandboxKind string

const (
	SandboxBool   SandboxKind = "bool"
	SandboxInt    SandboxKind = "int"
	SandboxFloat  SandboxKind = "float"
	SandboxString SandboxKind = "string"
)

type SandboxOption struct {
	Value int    `json:"value"`
	Label string `json:"label" doc:"empty when the file does not describe this choice"`
}

// SandboxEntry is one editable value. Min, Max, Default and Options come from
// the comments PZ writes above each key ("Minimum=0 Maximum=10 Default=1",
// "-- 1 = Insane"). PZ's comments omit the last choice of every list; it is
// added back, with an empty label unless the default names it.
type SandboxEntry struct {
	Key         string // dotted path below the root table, e.g. "ZombieLore.Speed"
	Value       string // decoded: strings are unquoted
	Kind        SandboxKind
	Description string
	Min, Max    *float64
	Default     string
	Options     []SandboxOption
	ReadOnly    bool // VERSION: PZ's own file-format version
}

var (
	ErrSandboxUnknownKey = errors.New("unknown sandbox option")
	ErrSandboxValue      = errors.New("invalid sandbox value")
)

type sbLine struct {
	rawLine
	entry   *SandboxEntry // nil unless the line holds an editable value
	prefix  string        // indent + name + " = "
	literal string        // the value as written
	suffix  string        // "," and trailing space
}

// Sandbox is a line-preserving <name>_SandboxVars.lua document. Only the
// literal of a `Key = value,` line is ever rewritten; everything else
// round-trips byte for byte.
type Sandbox struct {
	bom   bool
	eol   string
	lines []sbLine
}

var (
	sbOpen    = regexp.MustCompile(`^\s*([A-Za-z_]\w*)\s*=\s*\{\s*$`)
	sbClose   = regexp.MustCompile(`^\s*\}\s*,?\s*$`)
	sbValue   = regexp.MustCompile(`^(\s*([A-Za-z_]\w*)\s*=\s*)(true|false|-?\d+(?:\.\d+)?|-?\.\d+|"(?:[^"\\]|\\.)*")(\s*,?\s*)$`)
	sbEnum    = regexp.MustCompile(`^(-?\d+)\s*=\s*(.+?)\s*$`)
	sbMin     = regexp.MustCompile(`\s*Minimum=(-?[\d.]+)`)
	sbMax     = regexp.MustCompile(`\s*Maximum=(-?[\d.]+)`)
	sbDefault = regexp.MustCompile(`\s*Default=(.+?)\s*$`)
)

func ParseSandbox(r io.Reader) (*Sandbox, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	d := &Sandbox{}
	var raw []rawLine
	d.bom, raw, d.eol = splitLines(data)
	var stack, comment []string
	for n, l := range raw {
		line := sbLine{rawLine: l}
		t := strings.TrimSpace(l.text)
		switch {
		case strings.HasPrefix(t, "--"):
			comment = append(comment, strings.TrimPrefix(strings.TrimPrefix(t, "--"), " "))
		case sbOpen.MatchString(l.text):
			stack = append(stack, sbOpen.FindStringSubmatch(l.text)[1])
			comment = nil
		case sbClose.MatchString(l.text):
			if len(stack) == 0 {
				return nil, fmt.Errorf("sandbox vars: line %d: unexpected '}'", n+1)
			}
			stack = stack[:len(stack)-1]
			comment = nil
		case sbValue.MatchString(l.text):
			m := sbValue.FindStringSubmatch(l.text)
			path := m[2]
			if len(stack) > 1 {
				path = strings.Join(stack[1:], ".") + "." + path
			}
			line.prefix, line.literal, line.suffix = m[1], m[3], m[4]
			line.entry = newSandboxEntry(path, m[3], comment)
			comment = nil
		default:
			comment = nil
		}
		d.lines = append(d.lines, line)
	}
	if len(stack) > 0 {
		return nil, fmt.Errorf("sandbox vars: table %s is not closed", stack[len(stack)-1])
	}
	return d, nil
}

func ReadSandboxFile(path string) (*Sandbox, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read sandbox vars: %w", err)
	}
	defer f.Close()
	return ParseSandbox(f)
}

func newSandboxEntry(key, literal string, comment []string) *SandboxEntry {
	e := &SandboxEntry{Key: key, ReadOnly: key == "VERSION"}
	e.Kind, e.Value = decodeLiteral(literal)
	var desc []string
	for _, c := range comment {
		if m := sbEnum.FindStringSubmatch(c); m != nil {
			v, _ := strconv.Atoi(m[1])
			e.Options = append(e.Options, SandboxOption{Value: v, Label: m[2]})
			continue
		}
		if m := sbMin.FindStringSubmatch(c); m != nil {
			e.Min = parseBound(m[1])
		}
		if m := sbMax.FindStringSubmatch(c); m != nil {
			e.Max = parseBound(m[1])
		}
		if m := sbDefault.FindStringSubmatch(c); m != nil {
			e.Default = m[1]
		}
		c = sbDefault.ReplaceAllString(sbMax.ReplaceAllString(sbMin.ReplaceAllString(c, ""), ""), "")
		if c = strings.TrimSpace(c); c != "" {
			desc = append(desc, c)
		}
	}
	e.Description = strings.Join(desc, "\n")
	if e.Kind != SandboxInt {
		e.Options = nil
	}
	if len(e.Options) > 0 {
		// PZ's comments omit the last choice. Enum defaults are written as
		// labels, so a default matching no listed choice names that one.
		missing := SandboxOption{Value: e.Options[len(e.Options)-1].Value + 1}
		matched := false
		for _, o := range e.Options {
			if strings.EqualFold(o.Label, e.Default) {
				e.Default, matched = strconv.Itoa(o.Value), true
				break
			}
		}
		if !matched && e.Default != "" {
			missing.Label, e.Default = e.Default, strconv.Itoa(missing.Value)
		}
		e.Options = append(e.Options, missing)
	}
	return e
}

func parseBound(s string) *float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &f
}

func decodeLiteral(lit string) (SandboxKind, string) {
	switch {
	case lit == "true" || lit == "false":
		return SandboxBool, lit
	case strings.HasPrefix(lit, `"`):
		return SandboxString, luaUnquote(lit)
	case strings.Contains(lit, "."):
		return SandboxFloat, lit
	}
	return SandboxInt, lit
}

var luaUnescaper = strings.NewReplacer(`\\`, `\`, `\"`, `"`, `\n`, "\n", `\t`, "\t")

func luaUnquote(lit string) string { return luaUnescaper.Replace(lit[1 : len(lit)-1]) }

var luaEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// Entries lists the editable values in file order (first occurrence wins).
func (d *Sandbox) Entries() []SandboxEntry {
	seen := map[string]bool{}
	var out []SandboxEntry
	for _, l := range d.lines {
		if l.entry != nil && !seen[l.entry.Key] {
			seen[l.entry.Key] = true
			out = append(out, *l.entry)
		}
	}
	return out
}

func (d *Sandbox) Get(key string) (string, bool) {
	for _, l := range d.lines {
		if l.entry != nil && l.entry.Key == key {
			return l.entry.Value, true
		}
	}
	return "", false
}

// Set validates value against the key's kind, range and options, and
// rewrites its literal. It reports whether the file changed.
func (d *Sandbox) Set(key, value string) (bool, error) {
	changed, found := false, false
	for i := range d.lines {
		l := &d.lines[i]
		if l.entry == nil || l.entry.Key != key {
			continue
		}
		found = true
		if l.entry.ReadOnly {
			if value == l.entry.Value {
				continue
			}
			return false, fmt.Errorf("%w: %s is managed by the game", ErrSandboxValue, key)
		}
		lit, err := encodeLiteral(l.entry, value)
		if err != nil {
			return false, fmt.Errorf("%w: %s: %v", ErrSandboxValue, key, err)
		}
		if lit == l.literal {
			continue
		}
		l.literal, l.text = lit, l.prefix+lit+l.suffix
		_, l.entry.Value = decodeLiteral(lit)
		changed = true
	}
	if !found {
		return false, fmt.Errorf("%w: %s", ErrSandboxUnknownKey, key)
	}
	return changed, nil
}

func encodeLiteral(e *SandboxEntry, v string) (string, error) {
	switch e.Kind {
	case SandboxBool:
		if v != "true" && v != "false" {
			return "", errors.New("must be true or false")
		}
		return v, nil
	case SandboxString:
		if strings.ContainsAny(v, "\r\n") {
			return "", errors.New("cannot contain line breaks")
		}
		return `"` + luaEscaper.Replace(v) + `"`, nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return "", errors.New("must be a number")
	}
	if e.Min != nil && f < *e.Min || e.Max != nil && f > *e.Max {
		return "", fmt.Errorf("must be between %s and %s", fmtBound(e.Min), fmtBound(e.Max))
	}
	if e.Kind == SandboxInt {
		if f != math.Trunc(f) {
			return "", errors.New("must be a whole number")
		}
		return strconv.FormatInt(int64(f), 10), nil
	}
	lit := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(lit, ".") {
		lit += ".0"
	}
	return lit, nil
}

func fmtBound(b *float64) string {
	if b == nil {
		return "…"
	}
	return strconv.FormatFloat(*b, 'f', -1, 64)
}

func (d *Sandbox) Bytes() []byte { return joinLines(d.bom, d.lines) }

// WriteSandboxFileAtomic replaces path atomically, keeping an existing file's
// permissions (perm applies to a new file).
func WriteSandboxFileAtomic(path string, d *Sandbox, perm fs.FileMode) error {
	return renameio.WriteFile(path, d.Bytes(), perm)
}
