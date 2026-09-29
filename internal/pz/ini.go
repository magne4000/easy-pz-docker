package pz

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/google/renameio/v2"
)

type iniLine struct {
	rawLine
	isKV  bool
	key   string
	value string
}

// Ini is a line-preserving PZ server .ini document.
type Ini struct {
	bom   bool
	eol   string
	lines []iniLine
}

type IniEntry struct {
	Key, Value, Comment string
}

func NewIni() *Ini { return &Ini{eol: "\n"} }

func ParseIni(r io.Reader) (*Ini, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	d := NewIni()
	var raw []rawLine
	d.bom, raw, d.eol = splitLines(data)
	for _, l := range raw {
		d.lines = append(d.lines, parseIniLine(l))
	}
	return d, nil
}

func parseIniLine(raw rawLine) iniLine {
	l := iniLine{rawLine: raw}
	t := strings.TrimSpace(raw.text)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") {
		return l
	}
	k, v, ok := strings.Cut(t, "=")
	if !ok {
		return l
	}
	k = strings.TrimSpace(k)
	if k == "" {
		return l
	}
	l.isKV, l.key, l.value = true, k, strings.TrimSpace(v)
	return l
}

func ReadIniFile(path string) (*Ini, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read ini: %w", err)
	}
	defer f.Close()
	return ParseIni(f)
}

func (d *Ini) index(key string) int {
	for i, l := range d.lines {
		if l.isKV && l.key == key {
			return i
		}
	}
	return -1
}

func (d *Ini) Get(key string) (string, bool) {
	if i := d.index(key); i >= 0 {
		return d.lines[i].value, true
	}
	return "", false
}

func (d *Ini) Set(key, value string) {
	i := d.index(key)
	if i < 0 {
		if n := len(d.lines); n > 0 && d.lines[n-1].eol == "" {
			d.lines[n-1].eol = d.eol
		}
		d.lines = append(d.lines, iniLine{rawLine: rawLine{key + "=" + value, d.eol}, isKV: true, key: key, value: value})
		return
	}
	d.lines[i].text = key + "=" + value
	d.lines[i].value = value
	out := d.lines[:i+1]
	for _, l := range d.lines[i+1:] {
		if l.isKV && l.key == key {
			continue
		}
		out = append(out, l)
	}
	d.lines = out
}

func (d *Ini) SetIfMissing(key, value string) bool {
	if d.index(key) >= 0 {
		return false
	}
	d.Set(key, value)
	return true
}

func (d *Ini) Keys() []string {
	seen := map[string]bool{}
	var keys []string
	for _, l := range d.lines {
		if l.isKV && !seen[l.key] {
			seen[l.key] = true
			keys = append(keys, l.key)
		}
	}
	return keys
}

func (d *Ini) Entries() []IniEntry {
	seen := map[string]bool{}
	var out []IniEntry
	var comment []string
	for _, l := range d.lines {
		t := strings.TrimSpace(l.text)
		switch {
		case strings.HasPrefix(t, "#"):
			c := strings.TrimPrefix(t, "#")
			c = strings.TrimPrefix(c, " ")
			comment = append(comment, c)
		case l.isKV:
			if !seen[l.key] {
				seen[l.key] = true
				out = append(out, IniEntry{Key: l.key, Value: l.value, Comment: strings.Join(comment, "\n")})
			}
			comment = nil
		default:
			comment = nil
		}
	}
	return out
}

func (d *Ini) GetList(key string) []string {
	v, _ := d.Get(key)
	var out []string
	for _, p := range strings.Split(v, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (d *Ini) SetList(key string, items []string) {
	var clean []string
	for _, it := range items {
		if it = strings.TrimSpace(it); it != "" {
			clean = append(clean, it)
		}
	}
	d.Set(key, strings.Join(clean, ";"))
}

func (d *Ini) Bytes() []byte { return joinLines(d.bom, d.lines) }

// WriteIniFileAtomic replaces path atomically, keeping an existing file's
// permissions (perm applies to a new file).
func WriteIniFileAtomic(path string, d *Ini, perm fs.FileMode) error {
	return renameio.WriteFile(path, d.Bytes(), perm)
}
