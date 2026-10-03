package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/mods"
	"github.com/magne4000/easy-pz-docker/internal/pz"
	"github.com/magne4000/easy-pz-docker/internal/steam"
)

// Keys owned by other subsystems; the editor shows them read-only.
var managedKeys = []string{"Mods", "WorkshopItems", "Map", "BackupsOnStart", "BackupsOnVersionChange"}

type IniEntryView struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Comment string `json:"comment"`
	Managed bool   `json:"managed" doc:"owned by pzman (mods, backups); edit it elsewhere"`
}

type IniOutput struct {
	Body struct {
		Path    string         `json:"path"`
		Exists  bool           `json:"exists"`
		Entries []IniEntryView `json:"entries"`
	}
}

type IniUpdateInput struct {
	Body struct {
		Values map[string]string `json:"values" doc:"key → new value; unknown keys are appended"`
		Reload bool              `json:"reload,omitempty" doc:"ask the running server to reloadoptions after saving"`
	}
}

type IniUpdateOutput struct {
	Body struct {
		Changed  []string `json:"changed"`
		Reloaded bool     `json:"reloaded"`
		Message  string   `json:"message"`
	}
}

type SandboxEntryView struct {
	Key         string             `json:"key" doc:"dotted path below SandboxVars, e.g. ZombieLore.Speed"`
	Value       string             `json:"value" doc:"a mod option the file lacks yet shows its default"`
	Kind        pz.SandboxKind     `json:"kind" enum:"bool,int,float,string"`
	Label       string             `json:"label" doc:"mod options: the name the game's sandbox editor shows; empty when unknown"`
	Page        string             `json:"page" doc:"mod options: the game's sandbox editor page listing it; empty for the game's own options and unlisted ones"`
	Description string             `json:"description"`
	Default     string             `json:"default" doc:"from the file's comments, or the mod's sandbox-options.txt; empty when unknown"`
	Min         *float64           `json:"min,omitempty"`
	Max         *float64           `json:"max,omitempty"`
	Options     []pz.SandboxOption `json:"options" doc:"choices; for the game's own options, values outside them are allowed"`
	ReadOnly    bool               `json:"readOnly" doc:"managed by the game (VERSION)"`
}

type SandboxOutput struct {
	Body struct {
		Path     string             `json:"path"`
		Exists   bool               `json:"exists"`
		Entries  []SandboxEntryView `json:"entries"`
		Problems []string           `json:"problems" doc:"enabled mods' sandbox files that could not be read, wholly or partly"`
	}
}

type SandboxUpdateInput struct {
	Body struct {
		Values map[string]string `json:"values" doc:"key → new value; keys in the file or declared by an enabled mod"`
	}
}

type SandboxUpdateOutput struct {
	Body struct {
		Changed []string `json:"changed"`
		Message string   `json:"message"`
	}
}

type PathsOutput struct {
	Body struct {
		InstallDir   string `json:"installDir"`
		DataDir      string `json:"dataDir"`
		BackupDir    string `json:"backupDir"`
		IniPath      string `json:"iniPath"`
		SandboxPath  string `json:"sandboxPath"`
		ModsDir      string `json:"modsDir"`
		WorkshopDir  string `json:"workshopDir"`
		SaveDir      string `json:"saveDir"`
		DatabasePath string `json:"databasePath"`
	}
}

func sandboxPath(d Deps) string {
	return filepath.Join(d.Cfg.DataDir, "Server", d.Cfg.ServerName+"_SandboxVars.lua")
}

// readSandbox reads the SandboxVars file with the enabled mods' options, as
// the game's sandbox editor lists them.
func readSandbox(ctx context.Context, d Deps) (*pz.Sandbox, []error, error) {
	sb, err := pz.ReadSandboxFile(sandboxPath(d))
	if err != nil {
		return nil, nil, err
	}
	enabled, _, err := d.Mods.Enabled(ctx)
	if err != nil {
		return nil, nil, err
	}
	s := mods.LoadSandboxSchema(d.Cfg.InstallDir, enabled)
	sb.ApplyModOptions(s.Options, s.Text)
	return sb, s.Problems, nil
}

func iniPath(d Deps) string {
	return filepath.Join(d.Cfg.DataDir, "Server", d.Cfg.ServerName+".ini")
}

var keyRe = func(k string) bool {
	if k == "" || len(k) > 100 {
		return false
	}
	for _, r := range k {
		if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func registerConfig(api huma.API, d Deps) {
	huma.Register(api, op("get-ini", http.MethodGet, "/config/ini", "config", "The server .ini as structured entries"),
		func(ctx context.Context, _ *struct{}) (*IniOutput, error) {
			out := &IniOutput{}
			out.Body.Path, out.Body.Entries = iniPath(d), []IniEntryView{}
			ini, err := pz.ReadIniFile(iniPath(d))
			if errors.Is(err, fs.ErrNotExist) {
				return out, nil
			}
			if err != nil {
				return nil, mapErr(err)
			}
			out.Body.Exists = true
			for _, e := range ini.Entries() {
				managed := false
				for _, m := range managedKeys {
					managed = managed || m == e.Key
				}
				out.Body.Entries = append(out.Body.Entries, IniEntryView{Key: e.Key, Value: e.Value, Comment: e.Comment, Managed: managed})
			}
			return out, nil
		})
	huma.Register(api, op("update-ini", http.MethodPut, "/config/ini", "config", "Save ini values (and optionally reloadoptions)", 422),
		func(ctx context.Context, in *IniUpdateInput) (*IniUpdateOutput, error) {
			for k, v := range in.Body.Values {
				for _, m := range managedKeys {
					if k == m {
						return nil, huma.Error422UnprocessableEntity(k + " is managed by pzman and cannot be edited here")
					}
				}
				if !keyRe(k) {
					return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("invalid key %q", k))
				}
				if strings.ContainsAny(v, "\r\n") {
					return nil, huma.Error422UnprocessableEntity(k + ": values cannot contain line breaks")
				}
			}
			p := iniPath(d)
			ini, err := pz.ReadIniFile(p)
			if errors.Is(err, fs.ErrNotExist) {
				ini, err = pz.NewIni(), nil
			}
			if err != nil {
				return nil, mapErr(err)
			}
			out := &IniUpdateOutput{}
			out.Body.Changed = []string{}
			for k, v := range in.Body.Values {
				if cur, ok := ini.Get(k); ok && cur == v {
					continue
				}
				ini.Set(k, v)
				out.Body.Changed = append(out.Body.Changed, k)
			}
			if len(out.Body.Changed) > 0 {
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					return nil, mapErr(err)
				}
				if err := pz.WriteIniFileAtomic(p, ini, 0o644); err != nil {
					return nil, mapErr(err)
				}
				d.Bus.Publish(events.ConfigChanged{})
			}
			out.Body.Message = "saved; most options apply at the next restart"
			if in.Body.Reload && len(out.Body.Changed) > 0 {
				if _, err := d.Coord.Exec(ctx, "reloadoptions"); err == nil {
					out.Body.Reloaded, out.Body.Message = true, "saved and reloaded on the running server"
				} else {
					out.Body.Message = "saved; reload failed (" + err.Error() + "), options apply at the next restart"
				}
			}
			return out, nil
		})
	huma.Register(api, op("get-sandbox", http.MethodGet, "/config/sandbox", "config", "The SandboxVars.lua options as structured entries"),
		func(ctx context.Context, _ *struct{}) (*SandboxOutput, error) {
			out := &SandboxOutput{}
			out.Body.Path, out.Body.Entries, out.Body.Problems = sandboxPath(d), []SandboxEntryView{}, []string{}
			sb, problems, err := readSandbox(ctx, d)
			if errors.Is(err, fs.ErrNotExist) {
				return out, nil
			}
			if err != nil {
				return nil, mapErr(err)
			}
			out.Body.Exists = true
			for _, e := range sb.Entries() {
				out.Body.Entries = append(out.Body.Entries, SandboxEntryView{Key: e.Key, Value: e.Value, Kind: e.Kind, Label: e.Label, Page: e.Page,
					Description: e.Description, Default: e.Default, Min: e.Min, Max: e.Max, Options: append([]pz.SandboxOption{}, e.Options...), ReadOnly: e.ReadOnly})
			}
			for _, p := range problems {
				out.Body.Problems = append(out.Body.Problems, p.Error())
			}
			return out, nil
		})
	huma.Register(api, op("update-sandbox", http.MethodPut, "/config/sandbox", "config", "Save SandboxVars.lua values; they apply at the next start", 404, 422),
		func(ctx context.Context, in *SandboxUpdateInput) (*SandboxUpdateOutput, error) {
			p := sandboxPath(d)
			sb, _, err := readSandbox(ctx, d)
			if errors.Is(err, fs.ErrNotExist) {
				return nil, huma.Error404NotFound("the game has not created " + filepath.Base(p) + " yet")
			}
			if err != nil {
				return nil, mapErr(err)
			}
			out := &SandboxUpdateOutput{}
			out.Body.Changed = []string{}
			keys := slices.Sorted(maps.Keys(in.Body.Values))
			for _, k := range keys {
				changed, err := sb.Set(k, in.Body.Values[k])
				if err != nil {
					return nil, huma.Error422UnprocessableEntity(err.Error())
				}
				if changed {
					out.Body.Changed = append(out.Body.Changed, k)
				}
			}
			out.Body.Message = "nothing to save"
			if len(out.Body.Changed) > 0 {
				if err := pz.WriteSandboxFileAtomic(p, sb, 0o644); err != nil {
					return nil, mapErr(err)
				}
				d.Bus.Publish(events.ConfigChanged{})
				out.Body.Message = "saved; sandbox options apply at the next restart"
			}
			return out, nil
		})
	huma.Register(api, op("get-paths", http.MethodGet, "/config/paths", "config", "Resolved file locations"),
		func(ctx context.Context, _ *struct{}) (*PathsOutput, error) {
			out := &PathsOutput{}
			b := &out.Body
			n := d.Cfg.ServerName
			b.InstallDir, b.DataDir, b.BackupDir, b.IniPath = d.Cfg.InstallDir, d.Cfg.DataDir, d.Cfg.BackupDir, iniPath(d)
			b.SandboxPath = sandboxPath(d)
			b.ModsDir = filepath.Join(d.Cfg.DataDir, "mods")
			b.WorkshopDir = steam.WorkshopContentRoot(d.Cfg.InstallDir)
			b.SaveDir = filepath.Join(d.Cfg.DataDir, "Saves", "Multiplayer", n)
			b.DatabasePath = d.Cfg.DBPath
			return out, nil
		})
}
