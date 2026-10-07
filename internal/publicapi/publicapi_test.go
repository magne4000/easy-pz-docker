package publicapi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The names are the contract between the release workflow, self-update and the
// public page.
func TestLauncherAsset(t *testing.T) {
	require.Equal(t, "easypz-launcher-windows-amd64.exe", LauncherAsset("windows", "amd64"))
	require.Equal(t, "easypz-launcher-linux-arm64", LauncherAsset("linux", "arm64"))
	require.Equal(t, "easypz-launcher-darwin-universal.zip", LauncherAsset("darwin", "arm64"))
}
