package devfake

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaskPassword(t *testing.T) {
	args := []string{"-servername", "s", "-adminusername", "admin", "-adminpassword", "s3cret"}
	require.Equal(t, []string{"-servername", "s", "-adminusername", "admin", "-adminpassword", "***"}, maskPassword(args))
	require.Equal(t, "s3cret", args[5], "the launch arguments are not changed")
	require.Equal(t, []string{"-adminpassword"}, maskPassword([]string{"-adminpassword"}))
}
