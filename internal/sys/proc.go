package sys

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// Proc describes how pzman runs a child process (the game server, SteamCMD).
type Proc struct {
	Name string // for error messages
	Bin  string
	Args []string
	Dir  string
	Home string
	// UID and GID are the identity the child runs as when pzman runs as
	// root. A negative UID keeps pzman's own identity.
	UID, GID int
	// Stdin, when set, receives a pipe to the child's standard input.
	Stdin *io.WriteCloser
}

// Process is a started child. Output is stdout and stderr merged; Done
// yields Wait's result once the output has been closed.
type Process struct {
	Cmd    *exec.Cmd
	Output io.ReadCloser
	Done   <-chan error
}

// Start runs p in its own process group, so signals reach the whole tree the
// child spawns, with HOME set and root dropped to UID/GID. Cancelling ctx
// kills the group.
func (p Proc) Start(ctx context.Context) (*Process, error) {
	cmd := exec.CommandContext(ctx, p.Bin, p.Args...)
	cmd.Dir = p.Dir
	cmd.Env = append(os.Environ(), "HOME="+p.Home)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if p.UID >= 0 && os.Getuid() == 0 {
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(p.UID), Gid: uint32(p.GID)}
	}
	cmd.Cancel = func() error { return SignalGroup(cmd.Process.Pid, syscall.SIGKILL) }
	if p.Stdin != nil {
		in, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		*p.Stdin = in
	}
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: start: %w", p.Name, err)
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		pw.Close()
		done <- err
	}()
	return &Process{Cmd: cmd, Output: pr, Done: done}, nil
}

// SignalGroup signals every process in the group led by pid.
func SignalGroup(pid int, sig syscall.Signal) error { return syscall.Kill(-pid, sig) }
