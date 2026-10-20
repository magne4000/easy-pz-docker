package pz

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// startCommand is how the game server is launched: through the game's
// start-server.sh, or, when it is missing (the stock Pelican egg's install
// deletes it), ProjectZomboid64 itself with the environment the script's
// 64-bit branch sets up:
//
//	export PATH="${INSTDIR}/jre64/bin:$PATH"
//	export LD_LIBRARY_PATH="${INSTDIR}/linux64:${INSTDIR}/natives:${INSTDIR}:${INSTDIR}/jre64/lib/amd64:${LD_LIBRARY_PATH}"
//	LD_PRELOAD="${LD_PRELOAD}:libjsig.so" ./ProjectZomboid64 "$@"
//
// getenv reads pzman's environment (the values the script would extend).
func startCommand(installDir string, getenv func(string) string) (bin string, env []string, err error) {
	if installDir, err = filepath.Abs(installDir); err != nil { // the script's INSTDIR="`pwd`"
		return "", nil, err
	}
	script := filepath.Join(installDir, "start-server.sh")
	if _, err := os.Stat(script); err == nil {
		return script, nil, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", nil, fmt.Errorf("start-server.sh: %w", err)
	}
	bin = filepath.Join(installDir, "ProjectZomboid64")
	if _, err := os.Stat(bin); err != nil {
		return "", nil, fmt.Errorf("neither start-server.sh nor ProjectZomboid64 found (is the game installed?): %w", err)
	}
	env = []string{
		"PATH=" + filepath.Join(installDir, "jre64", "bin") + ":" + getenv("PATH"),
		"LD_LIBRARY_PATH=" + filepath.Join(installDir, "linux64") + ":" + filepath.Join(installDir, "natives") + ":" +
			installDir + ":" + filepath.Join(installDir, "jre64", "lib", "amd64") + ":" + getenv("LD_LIBRARY_PATH"),
		"LD_PRELOAD=" + getenv("LD_PRELOAD") + ":libjsig.so",
	}
	return bin, env, nil
}
