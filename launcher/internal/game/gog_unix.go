//go:build !windows

package game

// GOG registers installs only in the Windows registry.
func gogInstallDirs() []string { return nil }
