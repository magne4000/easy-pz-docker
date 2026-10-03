package pz

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func loadSandbox(t *testing.T, src []byte) *Sandbox {
	t.Helper()
	d, err := ParseSandbox(bytes.NewReader(src))
	require.NoError(t, err)
	return d
}

func TestSandboxRoundTrip(t *testing.T) {
	src, err := os.ReadFile("testdata/sandbox.lua")
	require.NoError(t, err)
	for _, in := range [][]byte{src, bytes.ReplaceAll(src, []byte("\n"), []byte("\r\n")), append([]byte("\xef\xbb\xbf"), src...)} {
		require.Equal(t, string(in), string(loadSandbox(t, in).Bytes()))
	}
}

func TestSandboxEntries(t *testing.T) {
	src, _ := os.ReadFile("testdata/sandbox.lua")
	d := loadSandbox(t, src)
	byKey := map[string]SandboxEntry{}
	var keys []string
	for _, e := range d.Entries() {
		byKey[e.Key] = e
		keys = append(keys, e.Key)
	}
	require.Equal(t, []string{"VERSION", "Zombies", "LockedHouses", "WaterShutModifier", "WorldItemRemovalList", "MultiHitZombies",
		"ZombieLore.Speed", "ZombieConfig.PopulationMultiplier", "RainCleansBlood.TilesPerMinuteNearPlayer",
		"RainCleansBlood.AlsoCleanAsh", "SomeMod.Nested.Flag"}, keys)

	z := byKey["Zombies"]
	require.Equal(t, SandboxInt, z.Kind)
	require.Equal(t, `Changing this also sets the "Population Multiplier" in Advanced Zombie Options.`, z.Description)
	require.Equal(t, "4", z.Default, "enum defaults resolve to the option value")
	require.Len(t, z.Options, 6, "B42 servers list every choice")
	require.Equal(t, SandboxOption{Value: 6, Label: "None"}, z.Options[5])
	require.Equal(t, "6", byKey["LockedHouses"].Default)

	w := byKey["WaterShutModifier"]
	require.Equal(t, -1.0, *w.Min)
	require.Equal(t, 2147483647.0, *w.Max)
	require.Equal(t, "14", w.Default)
	require.Equal(t, "How long after the default start date (July 9, 1993) that plumbing fixtures (eg. sinks) stop being infinite sources of water.", w.Description)

	p := byKey["ZombieConfig.PopulationMultiplier"]
	require.Equal(t, SandboxFloat, p.Kind)
	require.Equal(t, 4.0, *p.Max)
	require.Equal(t, "0.65", p.Default)

	require.Equal(t, SandboxString, byKey["WorldItemRemovalList"].Kind)
	require.Equal(t, "Base.Hat, Base.Glasses", byKey["WorldItemRemovalList"].Value)
	require.Equal(t, SandboxBool, byKey["SomeMod.Nested.Flag"].Kind)
}

// B41 servers wrote "Default=<label>" and left the last choice out of the
// list; the default naming it brings it back.
func TestSandboxEntriesB41Comments(t *testing.T) {
	d := loadSandbox(t, []byte("SandboxVars = {\n    -- Locked doors. Default=Very Often\n    -- 1 = Never\n    -- 2 = Rare\n    LockedHouses = 3,\n"+
		"    -- Days. Minimum=-1 Maximum=10 Default=4\n    Days = 4,\n}\n"))
	lh := d.Entries()[0]
	require.Equal(t, []SandboxOption{{1, "Never"}, {2, "Rare"}, {3, "Very Often"}}, lh.Options)
	require.Equal(t, "3", lh.Default)
	require.Equal(t, "Locked doors.", lh.Description)
	days := d.Entries()[1]
	require.Equal(t, -1.0, *days.Min)
	require.Equal(t, 10.0, *days.Max)
	require.Equal(t, "4", days.Default)
}

func TestSandboxSet(t *testing.T) {
	src, _ := os.ReadFile("testdata/sandbox.lua")
	d := loadSandbox(t, src)

	set := func(k, v string) error { _, err := d.Set(k, v); return err }
	require.NoError(t, set("ZombieLore.Speed", "1"))
	require.NoError(t, set("ZombieConfig.PopulationMultiplier", "2"))
	require.NoError(t, set("MultiHitZombies", "true"))
	require.NoError(t, set("WorldItemRemovalList", `Base."Odd"\Item`))
	require.NoError(t, set("LockedHouses", "7"), "enum lists are advisory")

	out := string(d.Bytes())
	require.Contains(t, out, "\n        Speed = 1,\n")
	require.Contains(t, out, "\n        PopulationMultiplier = 2.0,\n")
	require.Contains(t, out, "\n    MultiHitZombies = true,\n")
	require.Contains(t, out, `WorldItemRemovalList = "Base.\"Odd\"\\Item",`)
	// only the edited lines changed
	require.Equal(t, strings.Count(string(src), "\n"), strings.Count(out, "\n"))
	back := loadSandbox(t, d.Bytes())
	v, _ := back.Get("WorldItemRemovalList")
	require.Equal(t, `Base."Odd"\Item`, v)

	changed, err := d.Set("ZombieConfig.PopulationMultiplier", "2.0")
	require.NoError(t, err)
	require.False(t, changed, "an equivalent value is not a change")

	for k, v := range map[string]string{
		"MultiHitZombies": "yes", "ZombieLore.Speed": "1.5", "WaterShutModifier": "-2",
		"ZombieConfig.PopulationMultiplier": "4.5", "WorldItemRemovalList": "a\nb", "Zombies": "NaN",
	} {
		require.ErrorIs(t, set(k, v), ErrSandboxValue, k)
	}
	require.ErrorIs(t, set("Nope", "1"), ErrSandboxUnknownKey)
	require.ErrorIs(t, set("VERSION", "5"), ErrSandboxValue, "VERSION is the game's")
	require.NoError(t, set("VERSION", "6"), "resending the current value is fine")
}

func TestSandboxMalformed(t *testing.T) {
	for _, src := range []string{"SandboxVars = {\n    A = 1,\n", "}\n"} {
		_, err := ParseSandbox(strings.NewReader(src))
		require.Error(t, err, src)
	}
}
