package devfake

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const iniTemplate = `# Players can hurt and kill other players
PVP=true
# Clears the console log file and the server log every time the server starts
PauseEmpty=true
# Add a player to the whitelist automatically when they connect
Open=true
# Server description shown in the in-game browser
PublicDescription=A pzman development server
# Maximum number of players
MaxPlayers=16
# Minutes between each automatic world save
SaveWorldEveryMinutes=15
# Loot respawn in hours, 0 disables
HoursForLootRespawn=0
# Display name of the server
PublicName=pzman dev
Mods=
WorkshopItems=
Map=Muldraugh, KY
RCONPassword=
`

// Seed creates a miniature Zomboid tree (install + data) when absent, so the
// real file-handling code (ini, ACF, backup, links) runs against real files.
func Seed(installDir, dataDir, serverName string, sc Scenario) error {
	if _, err := os.Stat(filepath.Join(installDir, "steamapps", "appmanifest_380870.acf")); errors.Is(err, fs.ErrNotExist) {
		if err := WriteAppManifest(installDir, sc.InstalledBuild, ""); err != nil {
			return err
		}
		launch := `{
  "mainClass": "zombie/network/GameServer",
  "classpath": ["java/.", "java/projectzomboid.jar"],
  "vmArgs": ["-Djava.awt.headless=true", "-Xmx8g", "-Dzomboid.steam=1", "-Dzomboid.znetlog=1", "-Djava.library.path=linux64/:natives/", "-XX:+UseZGC"]
}
`
		if err := os.WriteFile(filepath.Join(installDir, "ProjectZomboid64.json"), []byte(launch), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(installDir, "start-server.sh"), []byte("#!/bin/sh\necho fake\n"), 0o755); err != nil {
			return err
		}
	}
	ini := filepath.Join(dataDir, "Server", serverName+".ini")
	if _, err := os.Stat(ini); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(ini), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(ini, []byte(iniTemplate), 0o644); err != nil {
			return err
		}
		os.WriteFile(filepath.Join(dataDir, "Server", serverName+"_SandboxVars.lua"), []byte("SandboxVars = {\n    VERSION = 5,\n    Zombies = 4,\n}\n"), 0o644)
	}
	save := filepath.Join(dataDir, "Saves", "Multiplayer", serverName)
	if _, err := os.Stat(save); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(save, 0o755); err != nil {
			return err
		}
		chunk := strings.Repeat("zomboid-chunk-data-", 4096)
		n := 12
		if sc.SlowBackup {
			n = 120
		}
		for i := range n {
			if err := os.WriteFile(filepath.Join(save, fmt.Sprintf("map_%d_%d.bin", i/8, i%8)), []byte(chunk), 0o644); err != nil {
				return err
			}
		}
		os.WriteFile(filepath.Join(save, "players.db"), []byte("players"), 0o644)
		os.MkdirAll(filepath.Join(dataDir, "db"), 0o755)
		os.WriteFile(filepath.Join(dataDir, "db", serverName+".db"), []byte("accounts"), 0o644)
	}
	return nil
}

// Tracker is what Setup needs from the mods service.
type Tracker interface {
	Add(ctx context.Context, ids []string) ([]string, error)
}

// Setup tracks the scenario's mods on first boot of a fresh dev database.
func Setup(ctx context.Context, t Tracker, sc Scenario, fresh bool) error {
	if !fresh || len(sc.Tracked) == 0 {
		return nil
	}
	_, err := t.Add(ctx, sc.Tracked)
	return err
}
