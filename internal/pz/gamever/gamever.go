// Package gamever reads the game version from projectzomboid.jar.
package gamever

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	coreClass    = "zombie/core/Core.class"
	versionClass = "zombie/core/GameVersion"
	versionInit  = "(IILjava/lang/String;)V"
	versionField = "gameVersion"
)

var ErrNotFound = errors.New("game version not found in Core.class")

type Version struct {
	Major, Minor int
	Suffix       string
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d%s", v.Major, v.Minor, v.Suffix)
}

func FromJar(path string) (Version, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Version{}, fmt.Errorf("open jar: %w", err)
	}
	defer zr.Close()
	f, err := zr.Open(coreClass)
	if err != nil {
		return Version{}, fmt.Errorf("open %s: %w", coreClass, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 16<<20))
	if err != nil {
		return Version{}, fmt.Errorf("read %s: %w", coreClass, err)
	}
	return FromClass(data)
}

// FromClass looks for Core.gameVersion = new GameVersion(major, minor, suffix).
func FromClass(data []byte) (Version, error) {
	cp, end, err := parseConstantPool(data)
	if err != nil {
		return Version{}, err
	}
	code := data[end:]
	for i := 0; i+4 <= len(code); i++ {
		if code[i] != opNew || code[i+3] != opDup || cp.className(u16(code[i+1:])) != versionClass {
			continue
		}
		if v, ok := matchInit(cp, code[i+4:]); ok {
			return v, nil
		}
	}
	return Version{}, ErrNotFound
}

const (
	opIconstM1    = 0x02
	opIconst5     = 0x08
	opBipush      = 0x10
	opSipush      = 0x11
	opLdc         = 0x12
	opLdcW        = 0x13
	opPutstatic   = 0xb3
	opInvokespec  = 0xb7
	opNew         = 0xbb
	opDup         = 0x59
	tagUtf8       = 1
	tagInteger    = 3
	tagFloat      = 4
	tagLong       = 5
	tagDouble     = 6
	tagClass      = 7
	tagString     = 8
	tagFieldref   = 9
	tagMethodref  = 10
	tagIMethodref = 11
	tagNameType   = 12
	tagMHandle    = 15
	tagMType      = 16
	tagDynamic    = 17
	tagInvokeDyn  = 18
	tagModule     = 19
	tagPackage    = 20
)

func matchInit(cp constantPool, b []byte) (Version, bool) {
	var v Version
	var ok bool
	if v.Major, b, ok = readInt(cp, b); !ok {
		return v, false
	}
	if v.Minor, b, ok = readInt(cp, b); !ok {
		return v, false
	}
	var idx uint16
	switch {
	case len(b) >= 2 && b[0] == opLdc:
		idx, b = uint16(b[1]), b[2:]
	case len(b) >= 3 && b[0] == opLdcW:
		idx, b = u16(b[1:]), b[3:]
	default:
		return v, false
	}
	if v.Suffix, ok = cp.string(idx); !ok {
		return v, false
	}
	if len(b) < 6 || b[0] != opInvokespec || b[3] != opPutstatic {
		return v, false
	}
	cls, name, desc := cp.memberRef(u16(b[1:]))
	if cls != versionClass || name != "<init>" || desc != versionInit {
		return v, false
	}
	if _, field, _ := cp.memberRef(u16(b[4:])); field != versionField {
		return v, false
	}
	return v, true
}

func readInt(cp constantPool, b []byte) (int, []byte, bool) {
	switch {
	case len(b) >= 1 && b[0] >= opIconstM1 && b[0] <= opIconst5:
		return int(b[0]) - 3, b[1:], true
	case len(b) >= 2 && b[0] == opBipush:
		return int(int8(b[1])), b[2:], true
	case len(b) >= 3 && b[0] == opSipush:
		return int(int16(u16(b[1:]))), b[3:], true
	case len(b) >= 2 && b[0] == opLdc:
		n, ok := cp.integer(uint16(b[1]))
		return n, b[2:], ok
	case len(b) >= 3 && b[0] == opLdcW:
		n, ok := cp.integer(u16(b[1:]))
		return n, b[3:], ok
	}
	return 0, b, false
}

func u16(b []byte) uint16 { return binary.BigEndian.Uint16(b) }

type constant struct {
	tag  byte
	a, b uint16 // referenced indexes
	utf8 string
	i32  int32
}

type constantPool []constant

func (cp constantPool) get(i uint16, tag byte) (constant, bool) {
	if int(i) <= 0 || int(i) >= len(cp) || cp[i].tag != tag {
		return constant{}, false
	}
	return cp[i], true
}

func (cp constantPool) utf8(i uint16) string {
	c, _ := cp.get(i, tagUtf8)
	return c.utf8
}

func (cp constantPool) className(i uint16) string {
	c, ok := cp.get(i, tagClass)
	if !ok {
		return ""
	}
	return cp.utf8(c.a)
}

func (cp constantPool) string(i uint16) (string, bool) {
	c, ok := cp.get(i, tagString)
	if !ok {
		return "", false
	}
	return cp.utf8(c.a), true
}

func (cp constantPool) integer(i uint16) (int, bool) {
	c, ok := cp.get(i, tagInteger)
	return int(c.i32), ok
}

func (cp constantPool) memberRef(i uint16) (string, string, string) {
	c, ok := cp.get(i, tagMethodref)
	if !ok {
		if c, ok = cp.get(i, tagFieldref); !ok {
			return "", "", ""
		}
	}
	nt, ok := cp.get(c.b, tagNameType)
	if !ok {
		return "", "", ""
	}
	return cp.className(c.a), cp.utf8(nt.a), cp.utf8(nt.b)
}

func parseConstantPool(data []byte) (constantPool, int, error) {
	if len(data) < 10 || binary.BigEndian.Uint32(data) != 0xCAFEBABE {
		return nil, 0, errors.New("not a class file")
	}
	n := int(u16(data[8:]))
	cp := make(constantPool, n)
	r := bytes.NewReader(data[10:])
	for i := 1; i < n; i++ {
		tag, err := r.ReadByte()
		if err != nil {
			return nil, 0, errTruncated
		}
		c := constant{tag: tag}
		switch tag {
		case tagUtf8:
			var l uint16
			if binary.Read(r, binary.BigEndian, &l) != nil {
				return nil, 0, errTruncated
			}
			s := make([]byte, l)
			if _, err := io.ReadFull(r, s); err != nil {
				return nil, 0, errTruncated
			}
			c.utf8 = string(s)
		case tagInteger, tagFloat:
			if binary.Read(r, binary.BigEndian, &c.i32) != nil {
				return nil, 0, errTruncated
			}
		case tagLong, tagDouble:
			if _, err := r.Seek(8, io.SeekCurrent); err != nil {
				return nil, 0, errTruncated
			}
			cp[i] = c
			i++ // 8-byte constants take two slots
			continue
		case tagClass, tagString, tagMType, tagModule, tagPackage:
			if binary.Read(r, binary.BigEndian, &c.a) != nil {
				return nil, 0, errTruncated
			}
		case tagFieldref, tagMethodref, tagIMethodref, tagNameType, tagDynamic, tagInvokeDyn:
			if binary.Read(r, binary.BigEndian, &c.a) != nil || binary.Read(r, binary.BigEndian, &c.b) != nil {
				return nil, 0, errTruncated
			}
		case tagMHandle:
			if _, err := r.Seek(3, io.SeekCurrent); err != nil {
				return nil, 0, errTruncated
			}
		default:
			return nil, 0, fmt.Errorf("unknown constant pool tag %d at #%d", tag, i)
		}
		cp[i] = c
	}
	return cp, len(data) - r.Len(), nil
}

var errTruncated = errors.New("truncated class file")
