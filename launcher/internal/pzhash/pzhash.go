// Package pzhash hashes passwords like the game: bcrypt(md5hex(pw)) with a fixed
// salt, which x/crypto/bcrypt cannot do.
package pzhash

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"

	"golang.org/x/crypto/blowfish" //nolint:staticcheck // bcrypt is built on Blowfish; this matches the game, it is not new crypto
)

const Salt = "$2a$12$O/BFHoDFPrfFaNPAACmWpu"

const (
	saltPrefix = "$2a$12$"
	cost       = 12
	saltLen    = 22 // base64 chars of the 16-byte salt
)

var b64 = base64.NewEncoding("./ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789").WithPadding(base64.NoPadding)

var magic = []byte("OrpheanBeholderScryDoubt")

func Hash(pw string) string {
	if pw == "" {
		return ""
	}
	sum := md5.Sum([]byte(pw))
	return bcryptFixedSalt([]byte(hex.EncodeToString(sum[:])))
}

func bcryptFixedSalt(key []byte) string {
	saltB64 := Salt[len(saltPrefix):]
	salt, err := b64.DecodeString(saltB64 + "..")
	if err != nil {
		panic("pzhash: bad salt: " + err.Error())
	}
	salt = salt[:16]

	ckey := append(append([]byte{}, key...), 0)
	if len(ckey) > 72 {
		ckey = ckey[:72]
	}
	c, err := blowfish.NewSaltedCipher(ckey, salt)
	if err != nil {
		panic("pzhash: " + err.Error())
	}
	for range 1 << cost {
		blowfish.ExpandKey(ckey, c)
		blowfish.ExpandKey(salt, c)
	}
	data := append([]byte{}, magic...)
	for i := 0; i < len(data); i += 8 {
		for range 64 {
			c.Encrypt(data[i:i+8], data[i:i+8])
		}
	}
	return saltPrefix + saltB64[:saltLen] + b64.EncodeToString(data[:23])
}
