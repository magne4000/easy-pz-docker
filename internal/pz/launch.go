package pz

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/renameio/v2"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// LaunchConfig is ProjectZomboid64.json; only vmArgs is rewritten, so unknown
// keys and key order are preserved.
type LaunchConfig struct {
	data   []byte
	vmArgs []string
}

func ReadLaunchConfig(path string) (*LaunchConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read launch config: %w", err)
	}
	return ParseLaunchConfig(data)
}

func ParseLaunchConfig(data []byte) (*LaunchConfig, error) {
	if !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() {
		return nil, fmt.Errorf("parse launch config: not a JSON object")
	}
	l := &LaunchConfig{data: data}
	if va := gjson.GetBytes(data, "vmArgs"); va.Exists() {
		if err := json.Unmarshal([]byte(va.Raw), &l.vmArgs); err != nil {
			return nil, fmt.Errorf("parse launch config vmArgs: %w", err)
		}
	}
	return l, nil
}

func (l *LaunchConfig) VMArgs() []string { return append([]string(nil), l.vmArgs...) }

func vmArgKey(arg string) string {
	switch {
	case strings.HasPrefix(arg, "-XX:+"), strings.HasPrefix(arg, "-XX:-"):
		return "-XX:" + arg[5:]
	case strings.HasPrefix(arg, "-XX:"):
		if i := strings.IndexByte(arg, '='); i >= 0 {
			return arg[:i+1]
		}
		return arg
	case strings.HasPrefix(arg, "-D"):
		if i := strings.IndexByte(arg, '='); i >= 0 {
			return arg[:i+1]
		}
		return arg + "="
	}
	for _, p := range []string{"-Xmx", "-Xms", "-Xss", "-Xmn"} {
		if strings.HasPrefix(arg, p) {
			return p
		}
	}
	return arg
}

func (l *LaunchConfig) SetVMArg(arg string) {
	key := vmArgKey(arg)
	out := make([]string, 0, len(l.vmArgs)+1)
	placed := false
	for _, a := range l.vmArgs {
		if vmArgKey(a) == key {
			if !placed {
				out = append(out, arg)
				placed = true
			}
			continue
		}
		out = append(out, a)
	}
	if !placed {
		out = append(out, arg)
	}
	l.vmArgs = out
}

func (l *LaunchConfig) Bytes() ([]byte, error) {
	args := l.vmArgs
	if args == nil {
		args = []string{}
	}
	data, err := sjson.SetBytes(l.data, "vmArgs", args)
	if err != nil {
		return nil, err
	}
	var compact, out bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return nil, err
	}
	if err := json.Indent(&out, compact.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func (l *LaunchConfig) Write(path string) error {
	data, err := l.Bytes()
	if err != nil {
		return err
	}
	return renameio.WriteFile(path, data, 0o644)
}

func ApplyLaunchSettings(l *LaunchConfig, xmxGB int, steam bool, extraVMArgs string) bool {
	before := strings.Join(l.vmArgs, "\x00")
	if xmxGB > 0 {
		l.SetVMArg("-Xmx" + strconv.Itoa(xmxGB) + "g")
	}
	if steam {
		l.SetVMArg("-Dzomboid.steam=1")
	} else {
		l.SetVMArg("-Dzomboid.steam=0")
	}
	for _, tok := range strings.Fields(extraVMArgs) {
		l.SetVMArg(tok)
	}
	return strings.Join(l.vmArgs, "\x00") != before
}
