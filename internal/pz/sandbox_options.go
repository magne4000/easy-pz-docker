package pz

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// SandboxOptionDef is one option a mod declares in media/sandbox-options.txt.
// Parsing mirrors zombie.sandbox.CustomSandboxOptions.
type SandboxOptionDef struct {
	Name             string  // "Option" or "Table.Option"
	Type             string  // boolean, integer, double, enum or string
	Default          string  // as a value of the matching SandboxKind: "true", "12", "0.5", "2", "text"
	Min, Max         float64 // integer and double
	NumValues        int     // enum: choices 1..NumValues
	Page             string  // the game's sandbox editor lists the option on this page; empty: not listed
	Translation      string  // Sandbox_<Translation> names it; empty: the name after the table
	ValueTranslation string  // enum: Sandbox_<ValueTranslation>_option<n> names choice n; empty: Translation
}

var luaName = regexp.MustCompile(`^[A-Za-z_]\w*(\.[A-Za-z_]\w*)?$`)

// ParseSandboxOptions reads a sandbox-options.txt the way the game does. Like
// the game, it keeps every option it can read: the error lists the options it
// skipped and why, and a wrong VERSION or an unknown block stops the parse.
func ParseSandboxOptions(r io.Reader) ([]SandboxOptionDef, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	// readFile joins the lines without a separator.
	s := strings.Map(func(c rune) rune {
		if c == '\r' || c == '\n' {
			return -1
		}
		return c
	}, string(data))
	root := &scriptBlock{}
	readScriptBlock(stripScriptComments(s), 0, root)
	v, _ := root.value("VERSION")
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err != nil || n != 1 {
		return nil, errors.New("invalid or missing VERSION")
	}
	var defs []SandboxOptionDef
	var errs []error
	for _, b := range root.children {
		if !strings.EqualFold(b.typ, "option") {
			errs = append(errs, fmt.Errorf("unknown block type %q", b.typ))
			break
		}
		def, err := parseSandboxOption(b)
		if err != nil {
			errs = append(errs, fmt.Errorf("option %q: %w", b.id, err))
			continue
		}
		defs = append(defs, def)
	}
	return defs, errors.Join(errs...)
}

func parseSandboxOption(b *scriptBlock) (SandboxOptionDef, error) {
	def := SandboxOptionDef{Name: b.id}
	// The game writes the option as `Table = { Option = … }` (one level), so
	// any other name would break the file.
	if !luaName.MatchString(def.Name) || def.Name == "VERSION" {
		return def, errors.New("the name must be Option or Table.Option")
	}
	typ, _ := b.value("type")
	def.Type = strings.TrimSpace(typ)
	def.Page, _ = b.value("page")
	def.Translation, _ = b.value("translation")
	def.ValueTranslation, _ = b.value("valueTranslation")
	def.Page, def.Translation, def.ValueTranslation = strings.TrimSpace(def.Page), strings.TrimSpace(def.Translation), strings.TrimSpace(def.ValueTranslation)

	dflt, hasDefault := b.value("default")
	switch def.Type {
	case "boolean", "string":
		if !hasDefault {
			return def, errors.New("missing default")
		}
		def.Default = strings.TrimSpace(dflt)
		if def.Type == "boolean" {
			def.Default = strconv.FormatBool(strings.EqualFold(def.Default, "true"))
		}
	case "integer":
		lo, ok1 := scriptInt(b, "min")
		hi, ok2 := scriptInt(b, "max")
		d, ok3 := scriptInt(b, "default")
		if !ok1 || !ok2 || !ok3 {
			return def, errors.New("min, max and default must be whole numbers")
		}
		def.Min, def.Max, def.Default = float64(lo), float64(hi), strconv.Itoa(d)
	case "double":
		lo, ok1 := scriptFloat(b, "min")
		hi, ok2 := scriptFloat(b, "max")
		d, ok3 := scriptFloat(b, "default")
		if !ok1 || !ok2 || !ok3 {
			return def, errors.New("min, max and default must be numbers")
		}
		def.Min, def.Max, def.Default = lo, hi, floatLiteral(d)
	case "enum":
		n, ok1 := scriptInt(b, "numValues")
		d, ok2 := scriptInt(b, "default")
		if !ok1 || !ok2 || n <= 0 || d <= 0 {
			return def, errors.New("numValues and default must be positive whole numbers")
		}
		def.NumValues, def.Default = n, strconv.Itoa(d)
	default:
		return def, fmt.Errorf("unknown type %q", def.Type)
	}
	return def, nil
}

// PZMath.tryParseInt: Integer.parseInt of the trimmed value.
func scriptInt(b *scriptBlock, key string) (int, bool) {
	v, _ := b.value(key)
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 32)
	return int(n), err == nil
}

// PZMath.tryParseDouble: Double.parseDouble of the trimmed value, which
// accepts a trailing f or d.
func scriptFloat(b *scriptBlock, key string) (float64, bool) {
	v, _ := b.value(key)
	if v = strings.TrimSpace(v); v != "" && strings.ContainsRune("fFdD", rune(v[len(v)-1])) {
		v = v[:len(v)-1]
	}
	f, err := strconv.ParseFloat(v, 64)
	return f, err == nil && !math.IsNaN(f)
}

// scriptBlock is a zombie.scripting.ScriptParser block.
type scriptBlock struct {
	typ, id  string
	values   []string
	children []*scriptBlock
}

// readScriptBlock ports ScriptParser.readBlock: '{' opens a child named by
// the "type id" before it, '}' closes the block, ',' ends a value. Text after
// a block's last ',' is dropped, and so is the character right after a '}'.
func readScriptBlock(s string, start int, b *scriptBlock) int {
	i := start
	for ; i < len(s); i++ {
		switch s[i] {
		case '{':
			child := &scriptBlock{}
			if f := strings.Fields(s[start:i]); len(f) > 0 {
				child.typ = f[0]
				if len(f) > 1 {
					child.id = f[1]
				}
			}
			b.children = append(b.children, child)
			i = readScriptBlock(s, i+1, child)
			start = i
		case '}':
			return i + 1
		case ',':
			b.values = append(b.values, s[start:i])
			start = i + 1
		}
	}
	return i
}

// value is ScriptParser.Block.getValue: the text after the first '=' of the
// first value whose trimmed key matches.
func (b *scriptBlock) value(key string) (string, bool) {
	for _, v := range b.values {
		if p := strings.IndexByte(v, '='); p > 0 && strings.TrimSpace(v[:p]) == key {
			return v[p+1:], true
		}
	}
	return "", false
}

// stripScriptComments ports ScriptParser.stripComments: removes /* */
// comments, nested ones included, scanning from the end.
func stripScriptComments(s string) string {
	end := lastIndexFrom(s, "*/", len(s))
	for end != -1 {
		start := lastIndexFrom(s, "/*", end-1)
		if start == -1 {
			break
		}
		innerEnd := lastIndexFrom(s, "*/", end-1)
		for innerEnd > start {
			innerStart := start
			if start = lastIndexFrom(s, "/*", start-2); start == -1 {
				break
			}
			innerEnd = lastIndexFrom(s, "*/", innerStart-2)
		}
		if start == -1 {
			break
		}
		s = s[:start] + s[end+2:]
		end = lastIndexFrom(s, "*/", start)
	}
	return s
}

// lastIndexFrom is Java's String.lastIndexOf(sub, from): the last match
// starting at or before from.
func lastIndexFrom(s, sub string, from int) int {
	if from < 0 {
		return -1
	}
	return strings.LastIndex(s[:min(from+len(sub), len(s))], sub)
}

// Translations is one of the game's translation tables (zombie.core.Translator):
// JSON files merged in load order, a later non-empty text replacing an earlier one.
type Translations map[string]string

// Load merges one translation file.
func (t Translations) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			s = fmt.Sprint(v)
		}
		if _, ok := t[k]; !ok || s != "" {
			t[k] = s
		}
	}
	return nil
}

var (
	rtBreak       = regexp.MustCompile(`(?i)<br>|<line>|\\n`)
	rtTag         = regexp.MustCompile(`<[A-Z][A-Z0-9_]*(?::[^>]*)?>`)
	rtLineSpace   = regexp.MustCompile(`[ \t]*\n[ \t]*`)
	textUnescaper = strings.NewReplacer(`\"`, `"`, "%%", "%")
)

// text is the translation of key as plain text, "" when there is none. The
// game formats it (%% is a %) and shows it in rich-text tooltips: <LINE>,
// <BR> and \n break lines, other tags (<RGB:1,0,0>, <BHC>…) only style them.
func (t Translations) text(key string) string {
	s := rtBreak.ReplaceAllString(t[key], "\n")
	s = rtLineSpace.ReplaceAllString(rtTag.ReplaceAllString(s, ""), "\n")
	return strings.TrimSpace(textUnescaper.Replace(s))
}
