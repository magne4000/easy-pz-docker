//go:build windows

package game

import (
	"os"

	"golang.org/x/sys/windows"
)

// steamInit loads Steam's library at lib and calls SteamAPI_InitSafe.
func steamInit(lib string) (bool, error) {
	dll, err := windows.LoadDLL(lib)
	if err != nil {
		return false, err
	}
	initSafe, err := dll.FindProc("SteamAPI_InitSafe")
	if err != nil {
		return false, err
	}
	r, _, _ := initSafe.Call()
	return byte(r) != 0, nil // a C++ bool: only the low byte is set
}

// waitExit returns once the process pid is gone. On Windows, any process can
// be waited on, not only a child.
func waitExit(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_, _ = p.Wait()
	}
}
