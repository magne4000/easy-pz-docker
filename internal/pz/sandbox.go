package pz

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"regexp"
	"slices"
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
	Label string `json:"label" doc:"empty when neither the file nor the mod describes this choice"`
}

// SandboxEntry is one editable value. For the game's own options, Min, Max,
// Default and Options come from the comments the server writes above each
// key ("Min: 0 Max: 10 Default: 1", "-- 1 = Insane"). Mod options take them,
// and their Label and Page, from the mod's declaration (ApplyModOptions).
type SandboxEntry struct {
	Key         string // dotted path below the root table, e.g. "ZombieLore.Speed"
	Value       string // decoded: strings are unquoted
	Kind        SandboxKind
	Label       string // mod options: the name the game's sandbox editor shows
	Page        string // mod options: the editor page listing it; empty: not listed
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
	depth   int           // a closing line: tables open before it (1: the root)
	closes  string        // a closing line: the table's dotted path below the root
}

// Sandbox is a line-preserving <name>_SandboxVars.lua document. Only the
// literal of a `Key = value,` line is ever rewritten, and lines are only
// added for mod options the file lacks; everything else round-trips byte
// for byte.
type Sandbox struct {
	bom   bool
	eol   string
	lines []sbLine
	mods  []*SandboxEntry // ApplyModOptions, in load order; Value is the default
}

var (
	sbOpen    = regexp.MustCompile(`^\s*([A-Za-z_]\w*)\s*=\s*\{\s*$`)
	sbClose   = regexp.MustCompile(`^\s*\}\s*,?\s*$`)
	sbValue   = regexp.MustCompile(`^(\s*([A-Za-z_]\w*)\s*=\s*)(true|false|-?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?|"(?:[^"\\]|\\.)*")(\s*,?\s*)$`)
	sbEnum    = regexp.MustCompile(`^(-?\d+)\s*=\s*(.+?)\s*$`)
	sbMin     = regexp.MustCompile(`\s*(?:Min:|Minimum=)\s*(-?[\d.]+)`)
	sbMax     = regexp.MustCompile(`\s*(?:Max:|Maximum=)\s*(-?[\d.]+)`)
	sbDefault = regexp.MustCompile(`\s*Default\s*[:=]\s*(.+?)\s*$`)
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
			line.depth, line.closes = len(stack), strings.Join(stack[1:], ".")
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
	// Enum defaults are written as labels. One naming no listed choice names
	// the one B41 servers left out of the list: the last.
	if len(e.Options) > 0 && e.Default != "" {
		i := slices.IndexFunc(e.Options, func(o SandboxOption) bool { return strings.EqualFold(o.Label, e.Default) })
		if i < 0 {
			e.Options = append(e.Options, SandboxOption{Value: e.Options[len(e.Options)-1].Value + 1, Label: e.Default})
			i = len(e.Options) - 1
		}
		e.Default = strconv.Itoa(e.Options[i].Value)
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
	case strings.ContainsAny(lit, ".eE"):
		return SandboxFloat, lit
	}
	return SandboxInt, lit
}

var luaUnescaper = strings.NewReplacer(`\\`, `\`, `\"`, `"`, `\n`, "\n", `\t`, "\t")

func luaUnquote(lit string) string { return luaUnescaper.Replace(lit[1 : len(lit)-1]) }

var luaEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// ApplyModOptions describes the options the enabled mods declare (in load
// order) the way the game's sandbox editor shows them: name, page and
// tooltip from the translations, exact type, range and choices from the
// declaration. A later declaration of a name replaces an earlier one, as in
// the game. An option the file lacks yet (the server writes every option at
// start) is listed with its default value, and Set adds it.
func (d *Sandbox) ApplyModOptions(defs []SandboxOptionDef, tr Translations) {
	index := map[string]int{}
	for _, m := range d.mods {
		index[m.Key] = len(index)
	}
	for _, def := range defs {
		e := def.entry(tr)
		if i, ok := index[e.Key]; ok {
			d.mods[i] = &e
			continue
		}
		index[e.Key] = len(d.mods)
		d.mods = append(d.mods, &e)
	}
	for i := range d.lines {
		l := &d.lines[i]
		if l.entry == nil {
			continue
		}
		if j, ok := index[l.entry.Key]; ok {
			e := *d.mods[j]
			e.Value = l.entry.Value
			e.Description = cmp.Or(e.Description, l.entry.Description)
			*l.entry = e
		}
	}
}

func (def SandboxOptionDef) entry(tr Translations) SandboxEntry {
	table, name := splitKey(def.Name)
	tk := cmp.Or(def.Translation, name)
	help := "_tooltip"
	if table == "ZombieConfig" && (def.Type == "integer" || def.Type == "double") {
		help = "_help"
	}
	e := SandboxEntry{Key: def.Name, Value: def.Default, Default: def.Default,
		Label: tr.text("Sandbox_" + tk), Description: tr.text("Sandbox_" + tk + help)}
	if def.Page != "" {
		e.Page = cmp.Or(tr.text("Sandbox_"+def.Page), def.Page)
	}
	switch def.Type {
	case "boolean":
		e.Kind = SandboxBool
	case "string":
		e.Kind = SandboxString
	case "integer", "double":
		e.Kind = SandboxInt
		if def.Type == "double" {
			e.Kind = SandboxFloat
		}
		e.Min, e.Max = &def.Min, &def.Max
	case "enum":
		e.Kind = SandboxInt
		lo, hi := 1.0, float64(def.NumValues)
		e.Min, e.Max = &lo, &hi
		vt := cmp.Or(def.ValueTranslation, tk)
		for n := 1; n <= def.NumValues; n++ {
			e.Options = append(e.Options, SandboxOption{Value: n, Label: tr.text(fmt.Sprintf("Sandbox_%s_option%d", vt, n))})
		}
	}
	return e
}

// splitKey is SandboxOptions.parseName: "Table.Option" or "Option".
func splitKey(key string) (table, name string) {
	if i := strings.IndexByte(key, '.'); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}

// Entries lists the editable values: the file's in file order (first
// occurrence wins), then the mods' in load order.
func (d *Sandbox) Entries() []SandboxEntry {
	seen := map[string]bool{}
	for _, m := range d.mods {
		seen[m.Key] = true
	}
	var out []SandboxEntry
	for _, l := range d.lines {
		if l.entry != nil && !seen[l.entry.Key] {
			seen[l.entry.Key] = true
			out = append(out, *l.entry)
		}
	}
	for _, m := range d.mods {
		out = append(out, *cmp.Or(d.line(m.Key), m))
	}
	return out
}

// line is the entry of key's first line, nil when the file lacks it.
func (d *Sandbox) line(key string) *SandboxEntry {
	for _, l := range d.lines {
		if l.entry != nil && l.entry.Key == key {
			return l.entry
		}
	}
	return nil
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
		return d.add(key, value)
	}
	return changed, nil
}

// add sets a mod option the file lacks. Its default needs no line: the
// server writes it at start.
func (d *Sandbox) add(key, value string) (bool, error) {
	i := slices.IndexFunc(d.mods, func(m *SandboxEntry) bool { return m.Key == key })
	if i < 0 {
		return false, fmt.Errorf("%w: %s", ErrSandboxUnknownKey, key)
	}
	e := *d.mods[i]
	lit, err := encodeLiteral(&e, value)
	if err != nil {
		return false, fmt.Errorf("%w: %s: %v", ErrSandboxValue, key, err)
	}
	if def, err := encodeLiteral(&e, e.Default); err == nil && def == lit {
		return false, nil
	}
	_, e.Value = decodeLiteral(lit)
	table, name := splitKey(key)
	depth := 2
	if table == "" {
		depth = 1
	}
	at := d.closing(table, depth)
	var add []sbLine
	if at < 0 {
		// The table is new: it goes last in the root, as the server writes mod tables.
		at = d.closing("", 1)
		add = append(add, sbLine{rawLine: rawLine{text: "    " + table + " = {"}})
	}
	v := sbLine{entry: &e, prefix: strings.Repeat("    ", depth) + name + " = ", literal: lit, suffix: ","}
	v.text = v.prefix + v.literal + v.suffix
	add = append(add, v)
	if len(add) > 1 {
		add = append(add, sbLine{rawLine: rawLine{text: "    },"}, depth: 2, closes: table})
	}
	for i := range add {
		add[i].eol = d.eol
	}
	d.separate(at)
	d.lines = slices.Insert(d.lines, at, add...)
	return true, nil
}

// closing is the index of the last line closing table at depth (Lua keeps
// the last of duplicate keys), -1 when there is none.
func (d *Sandbox) closing(table string, depth int) int {
	for i := len(d.lines) - 1; i >= 0; i-- {
		if l := d.lines[i]; l.depth == depth && l.closes == table {
			return i
		}
	}
	return -1
}

// separate lets a field follow the last value or table before line at: Lua
// needs a ',' between fields, which the server always writes but a hand
// edit may have dropped.
func (d *Sandbox) separate(at int) {
	for i := at - 1; i >= 0; i-- {
		l := &d.lines[i]
		t := strings.TrimSpace(l.text)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		switch {
		case strings.HasSuffix(t, ",") || strings.HasSuffix(t, ";"):
		case l.entry != nil:
			l.suffix = "," + l.suffix
			l.text = l.prefix + l.literal + l.suffix
		case l.depth > 0:
			l.text = strings.TrimRight(l.text, " \t") + ","
		}
		return
	}
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
	return floatLiteral(f), nil
}

// floatLiteral writes f the way the server writes a double: always with a
// decimal point.
func floatLiteral(f float64) string {
	lit := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(lit, ".") {
		lit += ".0"
	}
	return lit
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
