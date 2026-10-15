//go:build !windows

package game

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdbool.h>
#include <stdlib.h>

static bool call_bool(void *f) { return ((bool (*)(void))f)(); }
*/
import "C"

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

// steamInit loads Steam's library at lib and calls SteamAPI_InitSafe.
func steamInit(lib string) (bool, error) {
	cpath := C.CString(lib)
	defer C.free(unsafe.Pointer(cpath))
	h := C.dlopen(cpath, C.RTLD_NOW|C.RTLD_LOCAL)
	if h == nil {
		return false, errors.New(C.GoString(C.dlerror()))
	}
	cname := C.CString("SteamAPI_InitSafe")
	defer C.free(unsafe.Pointer(cname))
	f := C.dlsym(h, cname)
	if f == nil {
		return false, fmt.Errorf("%s: no SteamAPI_InitSafe", lib)
	}
	return bool(C.call_bool(f)), nil
}

// waitExit returns once the process pid is gone. It is not our child: poll.
func waitExit(pid int) {
	for syscall.Kill(pid, 0) == nil {
		time.Sleep(time.Second)
	}
}
