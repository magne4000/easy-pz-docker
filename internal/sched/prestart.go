package sched

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/magne4000/easy-pz-docker/internal/pz"
)

// PreStart re-applies everything Go owns before the JVM starts: launch JSON,
// seeded ini keys, missing mod downloads, mod links + Mods=, and the mod
// self-check. The supervisor calls it before every spawn.
func (c *Coordinator) PreStart(ctx context.Context) ([]string, error) {
	cfg := c.o.Cfg
	if err := c.PrepareConfig(); err != nil {
		return nil, err
	}
	if _, err := c.o.Mods.DownloadMissing(ctx); err != nil {
		c.o.Log.Warn("downloading missing workshop items before start failed", "err", err)
	}
	if _, err := c.o.Mods.Apply(ctx); err != nil {
		return nil, fmt.Errorf("apply mods: %w", err)
	}
	if err := c.o.Mods.Verify(ctx); err != nil {
		return nil, fmt.Errorf("mod self-check failed, refusing to start with missing mods: %w", err)
	}
	loaded, err := c.o.Mods.DesiredMods(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.loaded = loaded
	c.mu.Unlock()
	// Passed on every boot: without -adminusername PZ looks for the default
	// "admin" account and, when it is missing, blocks on a stdin password
	// prompt. PZ only uses the password to create a missing account.
	pw := cfg.GameAdminPassword
	if pw == "" {
		pw = randomToken(12)
		c.o.Log.Warn("ADMIN_PASSWORD is not set; generated an in-game admin password, used only if the account does not exist yet",
			"admin_username", cfg.GameAdminUser, "admin_password", pw)
	}
	return []string{"-adminusername", cfg.GameAdminUser, "-adminpassword", pw}, nil
}

// Loaded is Mods= as the running server was started with (nil when stopped).
func (c *Coordinator) Loaded() []string {
	if !running(c.o.Sup.Status().State) {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.loaded...)
}

func randomToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

// PrepareConfig seeds the ini (seed-if-missing, never overwrite),
// disables PZ's own ZipBackup and rewrites ProjectZomboid64.json.
func (c *Coordinator) PrepareConfig() error {
	cfg := c.o.Cfg
	iniPath := filepath.Join(cfg.DataDir, "Server", cfg.ServerName+".ini")
	ini, err := pz.ReadIniFile(iniPath)
	if errors.Is(err, fs.ErrNotExist) {
		ini, err = pz.NewIni(), nil
	}
	if err != nil {
		return err
	}
	before := string(ini.Bytes())
	ini.SetIfMissing("DefaultPort", strconv.Itoa(cfg.DefaultPort))
	ini.SetIfMissing("UDPPort", strconv.Itoa(cfg.UDPPort))
	ini.SetIfMissing("RCONPort", strconv.Itoa(cfg.RCONPort))
	ini.SetIfMissing("MaxPlayers", strconv.Itoa(cfg.MaxPlayers))
	if cfg.ServerPassword != "" {
		ini.SetIfMissing("Password", cfg.ServerPassword)
	}
	if cfg.SteamVAC != "" {
		ini.SetIfMissing("SteamVAC", cfg.SteamVAC)
	}
	// An empty RCONPassword makes RCON unusable, so empty counts as missing.
	if v, _ := ini.Get("RCONPassword"); v == "" {
		pw := cfg.RCONPassword
		if pw == "" {
			pw = randomToken(24)
		}
		ini.Set("RCONPassword", pw)
	}
	ini.Set("BackupsOnStart", "false")
	ini.Set("BackupsOnVersionChange", "false")
	if string(ini.Bytes()) != before {
		if err := os.MkdirAll(filepath.Dir(iniPath), 0o755); err != nil {
			return err
		}
		if err := pz.WriteIniFileAtomic(iniPath, ini, 0o644); err != nil {
			return err
		}
	}
	launchPath := filepath.Join(cfg.InstallDir, "ProjectZomboid64.json")
	lc, err := pz.ReadLaunchConfig(launchPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // not installed yet; spawn reports it
	}
	if err != nil {
		return err
	}
	if pz.ApplyLaunchSettings(lc, cfg.MemoryXmxGB, cfg.UseSteam, cfg.VMArgs) {
		return lc.Write(launchPath)
	}
	return nil
}

// RCONTarget reads the RCON address and password from the live ini.
func (c *Coordinator) RCONTarget() (string, string) {
	cfg := c.o.Cfg
	port, pw := strconv.Itoa(cfg.RCONPort), cfg.RCONPassword
	if ini, err := pz.ReadIniFile(filepath.Join(cfg.DataDir, "Server", cfg.ServerName+".ini")); err == nil {
		if v, ok := ini.Get("RCONPort"); ok && v != "" {
			port = v
		}
		if v, ok := ini.Get("RCONPassword"); ok {
			pw = v
		}
	}
	return "127.0.0.1:" + port, pw
}
