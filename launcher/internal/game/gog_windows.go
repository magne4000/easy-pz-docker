//go:build windows

package game

import "golang.org/x/sys/windows/registry"

// gogInstallDirs returns the install folders registered by GOG Galaxy and the
// GOG offline installer, wherever the user put their library.
func gogInstallDirs() []string {
	var out []string
	for _, key := range []string{
		`SOFTWARE\WOW6432Node\GOG.com\Games\` + gogProductID,
		`SOFTWARE\GOG.com\Games\` + gogProductID,
	} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, key, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		if p, _, err := k.GetStringValue("path"); err == nil && p != "" {
			out = append(out, p)
		}
		k.Close()
	}
	return out
}
