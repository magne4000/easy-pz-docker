package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeWarnMinutes(t *testing.T) {
	require.Equal(t, []int{15, 5, 1}, NormalizeWarnMinutes([]int{5, 0, 15, -3, 1, 5}))
	require.Equal(t, []int{}, NormalizeWarnMinutes(nil))
}
