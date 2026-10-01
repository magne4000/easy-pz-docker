package gamever

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// classBuilder emits a minimal class file: a constant pool followed by raw
// bytes standing in for the rest of the class (methods and their code).
type classBuilder struct {
	pool  bytes.Buffer
	count uint16
}

func newClass() *classBuilder { return &classBuilder{count: 1} }

func (c *classBuilder) add(tag byte, payload ...any) uint16 {
	c.pool.WriteByte(tag)
	for _, p := range payload {
		binary.Write(&c.pool, binary.BigEndian, p)
	}
	idx := c.count
	c.count++
	if tag == tagLong || tag == tagDouble {
		c.count++
	}
	return idx
}

func (c *classBuilder) utf8(s string) uint16 {
	c.pool.WriteByte(tagUtf8)
	binary.Write(&c.pool, binary.BigEndian, uint16(len(s)))
	c.pool.WriteString(s)
	idx := c.count
	c.count++
	return idx
}

func (c *classBuilder) class(name string) uint16 { return c.add(tagClass, c.utf8(name)) }

func (c *classBuilder) ref(tag byte, cls uint16, name, desc string) uint16 {
	nt := c.add(tagNameType, c.utf8(name), c.utf8(desc))
	return c.add(tag, cls, nt)
}

func (c *classBuilder) bytes(code []byte) []byte {
	var out bytes.Buffer
	binary.Write(&out, binary.BigEndian, uint32(0xCAFEBABE))
	binary.Write(&out, binary.BigEndian, uint32(65)) // minor, major
	binary.Write(&out, binary.BigEndian, c.count)
	out.Write(c.pool.Bytes())
	out.Write(code)
	return out.Bytes()
}

type coreRefs struct {
	gv, init, field, suffix, bigInt uint16
}

// coreClass builds a class shaped like zombie/core/Core with a few unrelated
// constants (long, double, method handle) that the pool parser must skip.
func buildCore() (*classBuilder, coreRefs) {
	c := newClass()
	c.add(tagLong, int64(1))
	c.add(tagDouble, float64(2))
	var r coreRefs
	r.gv = c.class(versionClass)
	r.init = c.ref(tagMethodref, r.gv, "<init>", versionInit)
	core := c.class("zombie/core/Core")
	r.field = c.ref(tagFieldref, core, versionField, "Lzombie/core/GameVersion;")
	r.suffix = c.add(tagString, c.utf8(" unstable"))
	r.bigInt = c.add(tagInteger, int32(1234))
	c.add(tagMHandle, byte(1), r.field)
	return c, r
}

func hi(i uint16) byte { return byte(i >> 8) }
func lo(i uint16) byte { return byte(i) }

func TestFromClass(t *testing.T) {
	c, r := buildCore()
	decoy := []byte{opNew, hi(r.gv), lo(r.gv), opDup, opIconst5} // incomplete sequence first
	code := append(decoy,
		opNew, hi(r.gv), lo(r.gv), opDup,
		opBipush, 42,
		opLdcW, hi(r.bigInt), lo(r.bigInt),
		opLdc, byte(r.suffix),
		opInvokespec, hi(r.init), lo(r.init),
		opPutstatic, hi(r.field), lo(r.field),
	)
	v, err := FromClass(c.bytes(code))
	require.NoError(t, err)
	require.Equal(t, Version{Major: 42, Minor: 1234, Suffix: " unstable"}, v)
	require.Equal(t, "42.1234 unstable", v.String())
}

func TestFromClassIconstAndSipush(t *testing.T) {
	c, r := buildCore()
	code := []byte{
		opNew, hi(r.gv), lo(r.gv), opDup,
		opSipush, 0x01, 0x00, // 256
		0x03, // iconst_0
		opLdc, byte(r.suffix),
		opInvokespec, hi(r.init), lo(r.init),
		opPutstatic, hi(r.field), lo(r.field),
	}
	v, err := FromClass(c.bytes(code))
	require.NoError(t, err)
	require.Equal(t, Version{Major: 256, Minor: 0, Suffix: " unstable"}, v)
}

func TestFromClassRejectsOtherField(t *testing.T) {
	c, r := buildCore()
	other := c.ref(tagFieldref, c.class("zombie/core/Core"), "otherVersion", "Lzombie/core/GameVersion;")
	code := []byte{
		opNew, hi(r.gv), lo(r.gv), opDup,
		opBipush, 42, opBipush, 21, opLdc, byte(r.suffix),
		opInvokespec, hi(r.init), lo(r.init),
		opPutstatic, hi(other), lo(other),
	}
	_, err := FromClass(c.bytes(code))
	require.ErrorIs(t, err, ErrNotFound)
}

func TestFromClassErrors(t *testing.T) {
	_, err := FromClass([]byte("nope"))
	require.Error(t, err)
	c, _ := buildCore()
	full := c.bytes(nil)
	_, err = FromClass(full[:len(full)-3])
	require.Error(t, err)
	_, err = FromClass(full)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestFromJar(t *testing.T) {
	c, r := buildCore()
	code := []byte{
		opNew, hi(r.gv), lo(r.gv), opDup,
		opBipush, 42, opBipush, 21, opLdc, byte(r.suffix),
		opInvokespec, hi(r.init), lo(r.init),
		opPutstatic, hi(r.field), lo(r.field),
	}
	path := filepath.Join(t.TempDir(), "projectzomboid.jar")
	f, err := os.Create(path)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	w, err := zw.Create(coreClass)
	require.NoError(t, err)
	_, err = w.Write(c.bytes(code))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	v, err := FromJar(path)
	require.NoError(t, err)
	require.Equal(t, "42.21 unstable", v.String())

	_, err = FromJar(filepath.Join(t.TempDir(), "missing.jar"))
	require.Error(t, err)
}

// PZ_JAR points at a real projectzomboid.jar (not redistributable, so not in
// the repo) to check the parser against the actual game.
func TestFromRealJar(t *testing.T) {
	path := os.Getenv("PZ_JAR")
	if path == "" {
		t.Skip("PZ_JAR not set")
	}
	v, err := FromJar(path)
	require.NoError(t, err)
	require.GreaterOrEqual(t, v.Major, 41)
	t.Logf("game version: %s", v)
}
