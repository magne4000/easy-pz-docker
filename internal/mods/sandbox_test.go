package mods

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSandboxSchema(t *testing.T) {
	root := t.TempDir()
	tr := func(dir string) string { return filepath.Join(dir, "media", "lua", "shared", "Translate", "EN", "Sandbox.json") }
	opts := func(dir string) string { return filepath.Join(dir, "media", "sandbox-options.txt") }
	install := filepath.Join(root, "install")
	write(t, tr(install), `{"Sandbox_Rarity_option1": "None", "Sandbox_X": "game X"}`)

	a := ModInfo{ID: "A", MediaDirs: []string{filepath.Join(root, "A", "common"), filepath.Join(root, "A", "42")}}
	write(t, opts(a.MediaDirs[0]), "VERSION = 1,\noption A.Old { type = boolean, default = true, }")
	write(t, opts(a.MediaDirs[1]), "VERSION = 1,\noption A.New { type = boolean, default = true, }")
	write(t, tr(a.MediaDirs[0]), `{"Sandbox_X": "common X", "Sandbox_Y": "common Y"}`)
	write(t, tr(a.MediaDirs[1]), `{"Sandbox_X": "version X", "Sandbox_Y": ""}`)

	b := ModInfo{ID: "B", MediaDirs: []string{filepath.Join(root, "B", "common")}}
	write(t, opts(b.MediaDirs[0]), "option B.Opt { type = boolean, default = true, }")
	write(t, tr(b.MediaDirs[0]), `{"Sandbox_Z": `)

	c := ModInfo{ID: "C", MediaDirs: []string{filepath.Join(root, "C", "common"), filepath.Join(root, "C", "42")}}
	write(t, opts(c.MediaDirs[0]), "VERSION = 1,\noption C.Opt { type = boolean, default = false, }")

	s := LoadSandboxSchema(install, []ModInfo{a, b, c})
	var names []string
	for _, o := range s.Options {
		names = append(names, o.Name)
	}
	require.Equal(t, []string{"A.New", "C.Opt"}, names, "the version dir's file wins; common is the fallback")
	require.Equal(t, "version X", s.Text["Sandbox_X"], "mods override the game, version dirs override common")
	require.Equal(t, "common Y", s.Text["Sandbox_Y"], "an empty text overrides nothing")
	require.Equal(t, "None", s.Text["Sandbox_Rarity_option1"], "mods can name the game's texts")
	require.Len(t, s.Problems, 2)
	require.ErrorContains(t, s.Problems[0], filepath.Join("B", "common", "media", "lua"))
	require.ErrorContains(t, s.Problems[1], "invalid or missing VERSION")
}
