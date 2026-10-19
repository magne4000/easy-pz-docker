package boot

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/magne4000/easy-pz-docker/internal/pz"
)

// stopCommand is what Pelican's Stop writes on stdin (the stock egg's stop command).
const stopCommand = "quit"

// consoleInput reads Pelican's console (pzman's stdin) line by line until EOF:
// "quit" calls quit (pzman's graceful shutdown), any other line goes to the
// game's console, whose reply reaches Pelican through the mirrored game
// output. EOF only ends the reading: Wings may close stdin, pzman keeps running.
func consoleInput(r io.Reader, sup pz.Supervisor, line func(string), quit func()) {
	br := bufio.NewReader(r)
	for {
		raw, err := br.ReadString('\n')
		if err != nil && raw == "" {
			return // EOF (or a read error): nothing more will come
		}
		cmd := strings.TrimSpace(raw)
		switch cmd {
		case "":
		case stopCommand:
			line("[pzman] stop requested: saving the world and stopping the game server")
			quit()
		default:
			err := sup.SendConsole(cmd)
			switch {
			case errors.Is(err, pz.ErrNotRunning):
				line(fmt.Sprintf("[pzman] the game server is not running; %q was not sent", cmd))
			case err != nil:
				line(fmt.Sprintf("[pzman] could not send %q to the game server: %v", cmd, err))
			}
		}
	}
}

// The marked block pzman owns in <root>/.pelicanignore; lines outside it are
// the user's and kept as they are.
const (
	ignoreBegin = "# BEGIN easy-pz (managed)"
	ignoreEnd   = "# END easy-pz (managed)"
)

// ignorePatterns keep Pelican backups to the world, pzman's database and
// settings: everything here is downloaded again (game, SteamCMD, mod packs)
// or pzman's own (its backups, logs). Gitignore syntax, anchored at the root
// so a mod's media/ under .cache/ stays in.
var ignorePatterns = []string{
	// the game install (SteamCMD app 380870 into the root)
	"/ProjectZomboid64*",
	"/start-server.sh",
	"/java/",
	"/jre64/",
	"/linux64/",
	"/natives/",
	"/media/",
	"/license/",
	"/steamapps/",
	"/Steam/",
	"/.steam/",
	"/*.so",
	"/*.jar",
	"/stdlib.lua",
	"/steam_appid.txt",
	// the egg's SteamCMD
	"/steamcmd/",
	// pzman's own backups and re-creatable state
	"/.easy-pz/backups/",
	"/.easy-pz/cache/",
	"/.easy-pz/steam/",
	"/.easy-pz/logs/",
}

// writePelicanIgnore writes or refreshes pzman's block in root/.pelicanignore.
// It rewrites the file only when the block changed.
func writePelicanIgnore(root string) error {
	path := filepath.Join(root, ".pelicanignore")
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	next := withIgnoreBlock(string(old))
	if next == string(old) {
		return nil
	}
	return os.WriteFile(path, []byte(next), 0o644)
}

// withIgnoreBlock returns content with pzman's block replaced, or appended
// when absent; every line outside the block is kept.
func withIgnoreBlock(content string) string {
	block := ignoreBegin + "\n" + strings.Join(ignorePatterns, "\n") + "\n" + ignoreEnd + "\n"
	lines := strings.SplitAfter(content, "\n")
	var b strings.Builder
	in, placed := false, false
	for _, l := range lines {
		switch t := strings.TrimSpace(l); {
		case !in && t == ignoreBegin:
			in = true
		case in && t == ignoreEnd:
			in = false
			if !placed {
				b.WriteString(block)
				placed = true
			}
		case !in:
			b.WriteString(l)
		}
	}
	if in && !placed { // an unterminated block: replace it up to the end
		b.WriteString(block)
		placed = true
	}
	if !placed {
		s := b.String()
		if s != "" && !strings.HasSuffix(s, "\n") {
			b.WriteString("\n")
		}
		b.WriteString(block)
	}
	return b.String()
}
