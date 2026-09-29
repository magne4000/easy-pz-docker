package pz

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIniRoundTrip(t *testing.T) {
	for _, in := range []string{
		"",
		"# comment\nPVP=true\n",
		"A=1\r\nB = two\r\n",
		"\xef\xbb\xbfA=1\nno newline=x",
		"weird line\n\nKey=\n",
	} {
		d, err := ParseIni(strings.NewReader(in))
		require.NoError(t, err)
		require.Equal(t, in, string(d.Bytes()))
	}
}

func TestIniSpacingAndSet(t *testing.T) {
	d, _ := ParseIni(strings.NewReader("# Mods\nMods = A;B ; \nWorkshopItems=1\r\nMods=dup\n"))
	v, ok := d.Get("Mods")
	require.True(t, ok)
	require.Equal(t, "A;B ;", v)
	require.Equal(t, []string{"A", "B"}, d.GetList("Mods"))
	d.SetList("Mods", []string{"C", " ", "D"})
	require.Equal(t, "# Mods\nMods=C;D\nWorkshopItems=1\r\n", string(d.Bytes()))
	require.True(t, d.SetIfMissing("RCONPort", "27015"))
	require.False(t, d.SetIfMissing("Mods", "x"))
	require.Equal(t, "Mods", d.Entries()[0].Key)
	require.Equal(t, "Mods", d.Entries()[0].Comment)
}

func TestLaunchConfig(t *testing.T) {
	l, err := ParseLaunchConfig([]byte(`{"mainClass":"zombie/network/GameServer","vmArgs":["-Djava.awt.headless=true","-Xmx8g","-Dzomboid.steam=1","-XX:+UseZGC"],"classpath":["a.jar"]}`))
	require.NoError(t, err)
	require.True(t, ApplyLaunchSettings(l, 4, false, "-XX:-UseZGC -Dzomboid.steam="))
	require.Equal(t, []string{"-Djava.awt.headless=true", "-Xmx4g", "-Dzomboid.steam=", "-XX:-UseZGC"}, l.VMArgs())
	require.False(t, ApplyLaunchSettings(l, 4, false, "-XX:-UseZGC -Dzomboid.steam="))
	b, _ := l.Bytes()
	require.True(t, strings.Index(string(b), "mainClass") < strings.Index(string(b), "classpath"))
}
