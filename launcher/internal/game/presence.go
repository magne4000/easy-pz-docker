package game

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
)

// Steam presence: -nosteam keeps the game itself off Steam, so while a game
// started by Start runs, a process of the launcher registers with Steam as
// Project Zomboid through the game's own Steam library, and friends see the
// player in game. Steam ends a presence only when the registered process exits
// (SteamAPI_Shutdown keeps it), so that process watches the game and exits with
// it. It is detached like the game: closing the launcher keeps it, and its
// failure never touches the game.
//
// Never route this through Steam's own launch (steam://run, -applaunch,
// SteamAPI_RestartAppIfNecessary) nor set the "connect" rich presence: the
// game's Steam launch options may point at this launcher, which would then
// start itself.

// SteamPresenceArg, followed by the library path and the game's PID, makes
// main run ServeSteamPresence instead of the launcher.
const SteamPresenceArg = "--steam-presence"

const steamAppID = "108600"

// startPresence starts the presence process for the game running as pid.
// Best effort: the game runs regardless.
func startPresence(steamLib string, pid int) {
	exe, err := os.Executable()
	if err != nil {
		slog.Info("no Steam presence", "err", err)
		return
	}
	cmd := exec.Command(exe, SteamPresenceArg, steamLib, strconv.Itoa(pid))
	cmd.Stderr = os.Stderr
	detach(cmd)
	if err := cmd.Start(); err != nil {
		slog.Info("no Steam presence", "err", err)
		return
	}
	go func() { _ = cmd.Wait() }()
}

// ServeSteamPresence registers this process with Steam as Project Zomboid,
// using Steam's library at lib, until the process pid exits. It returns the
// exit code.
func ServeSteamPresence(lib, pid string) int {
	p, err := strconv.Atoi(pid)
	if err == nil {
		// Steam reads the app from the environment; no other thread reads it yet.
		err = os.Setenv("SteamAppId", steamAppID)
	}
	if err == nil {
		var ok bool
		if ok, err = steamInit(lib); err == nil && !ok {
			err = errors.New("SteamAPI_InitSafe failed: Steam is not running, or this account does not own the game")
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "no Steam presence:", err)
		return 1
	}
	waitExit(p)
	return 0
}
