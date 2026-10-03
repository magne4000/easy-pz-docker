package mods

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/magne4000/easy-pz-docker/internal/pz"
)

// SandboxSchema is what the enabled mods add to the game's sandbox editor.
type SandboxSchema struct {
	Options  []pz.SandboxOptionDef
	Text     pz.Translations
	Problems []error // files the game would also fail to read, wholly or partly
}

// LoadSandboxSchema reads it the way the game does, for the mods in load
// order: each mod's media/sandbox-options.txt from its version dir, else from
// common (CustomSandboxOptions.init), and the English Sandbox translations of
// the game, then of every mod, common before the version dir (Translator).
func LoadSandboxSchema(installDir string, enabled []ModInfo) SandboxSchema {
	s := SandboxSchema{Text: pz.Translations{}}
	translate := func(dir string) {
		err := s.Text.Load(filepath.Join(dir, "media", "lua", "shared", "Translate", "EN", "Sandbox.json"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			s.Problems = append(s.Problems, err)
		}
	}
	translate(installDir)
	for _, m := range enabled {
		for _, dir := range m.MediaDirs {
			translate(dir)
		}
		for i := len(m.MediaDirs) - 1; i >= 0; i-- {
			p := filepath.Join(m.MediaDirs[i], "media", "sandbox-options.txt")
			f, err := os.Open(p)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err == nil {
				var defs []pz.SandboxOptionDef
				defs, err = pz.ParseSandboxOptions(f)
				f.Close()
				s.Options = append(s.Options, defs...)
			}
			if err != nil {
				s.Problems = append(s.Problems, fmt.Errorf("%s: %w", p, err))
			}
			break
		}
	}
	return s
}
