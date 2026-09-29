package steam

import (
	"fmt"
	"io"
	"strings"

	"github.com/andygrunwald/vdf"
)

// KV is a block of a Valve KeyValues (VDF/ACF) document.
type KV map[string]any

// ParseVDF parses a KeyValues text document holding one root block.
func ParseVDF(r io.Reader) (KV, error) {
	m, err := vdf.NewParser(r).Parse()
	if err != nil {
		return nil, fmt.Errorf("vdf: %w", err)
	}
	return m, nil
}

func (n KV) lookup(key string) (any, bool) {
	if v, ok := n[key]; ok {
		return v, true
	}
	for k, v := range n {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

// Get walks blocks by key, case-insensitively; nil when absent.
func (n KV) Get(path ...string) KV {
	cur := n
	for _, p := range path {
		v, _ := cur.lookup(p)
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		cur = m
	}
	return cur
}

// String returns the leaf value at path, or "" when absent.
func (n KV) String(path ...string) string {
	if len(path) == 0 {
		return ""
	}
	v, _ := n.Get(path[:len(path)-1]...).lookup(path[len(path)-1])
	s, _ := v.(string)
	return s
}
