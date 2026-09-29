package pzhash

import (
	"crypto/md5"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestHashIsValidBcryptOfMD5Hex(t *testing.T) {
	for _, pw := range []string{"secret", "pässwörd with spaces", strings.Repeat("x", 100)} {
		h := Hash(pw)
		require.True(t, strings.HasPrefix(h, Salt), h)
		require.Len(t, h, 60)
		sum := md5.Sum([]byte(pw))
		require.NoError(t, bcrypt.CompareHashAndPassword([]byte(h), []byte(hex.EncodeToString(sum[:]))), pw)
		require.Error(t, bcrypt.CompareHashAndPassword([]byte(h), []byte(pw)))
	}
}

// Produced by the game's own jBCrypt (projectzomboid.jar, 42.21):
// BCrypt.hashpw(md5hex("secret"), Salt).
func TestHashMatchesGame(t *testing.T) {
	require.Equal(t, "$2a$12$O/BFHoDFPrfFaNPAACmWpuHNSkpy8/pjTKuFeFrQoia1UzbL3/i8y", Hash("secret"))
}

func TestHashIsDeterministic(t *testing.T) {
	require.Equal(t, Hash("secret"), Hash("secret"))
	require.NotEqual(t, Hash("secret"), Hash("Secret"))
	require.Empty(t, Hash(""))
}
