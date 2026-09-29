package pz

import (
	"bytes"
	"strings"
)

// The ini and SandboxVars codecs keep every line as read and rewrite only
// what they edit, so a file round-trips byte for byte.

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

type rawLine struct {
	text string // without terminator
	eol  string // "", "\n" or "\r\n"
}

// splitLines splits a text file keeping each line's terminator, so it can be
// written back byte for byte. eol is the file's dominant terminator.
func splitLines(data []byte) (bom bool, lines []rawLine, eol string) {
	if bytes.HasPrefix(data, utf8BOM) {
		bom = true
		data = data[len(utf8BOM):]
	}
	s := string(data)
	crlf, lf := 0, 0
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		l := rawLine{}
		if i < 0 {
			l.text, s = s, ""
		} else {
			l.text, s, l.eol = s[:i], s[i+1:], "\n"
			if strings.HasSuffix(l.text, "\r") {
				l.text, l.eol = l.text[:len(l.text)-1], "\r\n"
				crlf++
			} else {
				lf++
			}
		}
		lines = append(lines, l)
	}
	eol = "\n"
	if crlf > lf {
		eol = "\r\n"
	}
	return bom, lines, eol
}

func (l rawLine) line() rawLine { return l }

// joinLines is splitLines' inverse.
func joinLines[L interface{ line() rawLine }](bom bool, lines []L) []byte {
	var b bytes.Buffer
	if bom {
		b.Write(utf8BOM)
	}
	for _, l := range lines {
		r := l.line()
		b.WriteString(r.text)
		b.WriteString(r.eol)
	}
	return b.Bytes()
}
