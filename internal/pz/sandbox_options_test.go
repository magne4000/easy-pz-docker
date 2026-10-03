package pz

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const rcbOptions = `VERSION = 1,
/* Rain Cleans Blood, with a comma, and /* nested */ comments */
option RainCleansBlood.TilesPerMinuteNearPlayer
{
	type = integer, min = 10, max = 2000, default = 300,
	page = RainCleansBlood, translation = RCB_Tiles,
}
option RainCleansBlood.Mode
{
	type = enum, numValues = 3, default = 2,
	page = RainCleansBlood, translation = RCB_Mode, valueTranslation = RCB_Modes,
}
option NewMod.Rate
{
	type = double, min = 0, max = 2.5, default = 1f,
	page = NewMod,
}
option NewMod.Enabled { type = boolean, default = TRUE, }
option NewMod.NoMax
{
	type = integer, min = 1, default = 2,
}
option NewMod.Last
{
	type = string, default = x
}
`

func TestParseSandboxOptions(t *testing.T) {
	defs, err := ParseSandboxOptions(strings.NewReader(rcbOptions))
	require.ErrorContains(t, err, `option "NewMod.NoMax": min, max and default must be whole numbers`)
	require.ErrorContains(t, err, `option "NewMod.Last": missing default`, "like the game, a value needs a trailing comma")
	require.Equal(t, []SandboxOptionDef{
		{Name: "RainCleansBlood.TilesPerMinuteNearPlayer", Type: "integer", Default: "300", Min: 10, Max: 2000, Page: "RainCleansBlood", Translation: "RCB_Tiles"},
		{Name: "RainCleansBlood.Mode", Type: "enum", Default: "2", NumValues: 3, Page: "RainCleansBlood", Translation: "RCB_Mode", ValueTranslation: "RCB_Modes"},
		{Name: "NewMod.Rate", Type: "double", Default: "1.0", Max: 2.5, Page: "NewMod"},
		{Name: "NewMod.Enabled", Type: "boolean", Default: "true"},
	}, defs)

	defs, err = ParseSandboxOptions(strings.NewReader("option A.B { type = boolean, default = true, }"))
	require.EqualError(t, err, "invalid or missing VERSION")
	require.Empty(t, defs)

	defs, err = ParseSandboxOptions(strings.NewReader("VERSION = 1,\noption A.B { type = boolean, default = true, }\nmodule X { }\noption A.C { type = boolean, default = true, }"))
	require.ErrorContains(t, err, `unknown block type "module"`)
	require.Len(t, defs, 1, "the game stops at an unknown block")

	_, err = ParseSandboxOptions(strings.NewReader("VERSION = 1,\noption A.B.C { type = boolean, default = true, }"))
	require.ErrorContains(t, err, "Option or Table.Option", "the game would write an invalid file")
}

func rcbTranslations() Translations {
	return Translations{
		"Sandbox_RainCleansBlood":   "Rain Cleans Blood",
		"Sandbox_RCB_Tiles":         "Tiles per minute",
		"Sandbox_RCB_Tiles_tooltip": `How many tiles get cleaned. <LINE> <RGB:1,0,0> Up to 100%% \"fast\"`,
		"Sandbox_RCB_Mode":          "Mode",
		"Sandbox_RCB_Modes_option1": "Off",
		"Sandbox_RCB_Modes_option2": "Rain<br>only",
	}
}

func TestSandboxModOptions(t *testing.T) {
	src, _ := os.ReadFile("testdata/sandbox.lua")
	d := loadSandbox(t, src)
	defs, _ := ParseSandboxOptions(strings.NewReader(rcbOptions))
	d.ApplyModOptions(defs, rcbTranslations())

	es := d.Entries()
	var keys []string
	byKey := map[string]SandboxEntry{}
	for _, e := range es {
		keys = append(keys, e.Key)
		byKey[e.Key] = e
	}
	require.Equal(t, []string{"RainCleansBlood.AlsoCleanAsh", "SomeMod.Nested.Flag", "RainCleansBlood.TilesPerMinuteNearPlayer",
		"RainCleansBlood.Mode", "NewMod.Rate", "NewMod.Enabled"}, keys[len(keys)-6:], "mod options last, in declaration order")

	tiles := byKey["RainCleansBlood.TilesPerMinuteNearPlayer"]
	require.Equal(t, "300", tiles.Value, "the file's value")
	require.Equal(t, "Tiles per minute", tiles.Label)
	require.Equal(t, "Rain Cleans Blood", tiles.Page)
	require.Equal(t, "How many tiles get cleaned.\nUp to 100% \"fast\"", tiles.Description)
	require.Equal(t, 10.0, *tiles.Min)

	mode := byKey["RainCleansBlood.Mode"]
	require.Equal(t, "2", mode.Value, "the default until the server writes it")
	require.Equal(t, []SandboxOption{{1, "Off"}, {2, "Rain\nonly"}, {3, ""}}, mode.Options)

	rate := byKey["NewMod.Rate"]
	require.Equal(t, SandboxFloat, rate.Kind)
	require.Equal(t, "NewMod", rate.Page, "an untranslated page keeps its name")
	require.Empty(t, rate.Label)
	require.Empty(t, byKey["NewMod.Enabled"].Page, "no page: the game's editor does not list it")

	set := func(k, v string) (bool, error) { return d.Set(k, v) }
	_, err := set("RainCleansBlood.TilesPerMinuteNearPlayer", "5")
	require.ErrorIs(t, err, ErrSandboxValue, "the declared range applies")
	_, err = set("RainCleansBlood.Mode", "4")
	require.ErrorIs(t, err, ErrSandboxValue, "enum choices are exact")
	changed, err := set("NewMod.Rate", "1")
	require.NoError(t, err)
	require.False(t, changed, "the default needs no line")
	require.Equal(t, string(src), string(d.Bytes()))

	// In order: the second option of a new table goes into the table the first created.
	for _, kv := range [][2]string{{"RainCleansBlood.Mode", "3"}, {"NewMod.Rate", "2"}, {"NewMod.Enabled", "false"}} {
		changed, err := set(kv[0], kv[1])
		require.NoError(t, err, kv[0])
		require.True(t, changed, kv[0])
	}
	out := string(d.Bytes())
	require.Contains(t, out, "        AlsoCleanAsh = true,\n        Mode = 3,\n    },\n    SomeMod = {\n")
	require.True(t, strings.HasSuffix(out, "    },\n    NewMod = {\n        Rate = 2.0,\n        Enabled = false,\n    },\n}\n"), out)

	back := loadSandbox(t, d.Bytes())
	back.ApplyModOptions(defs, nil)
	for k, v := range map[string]string{"RainCleansBlood.Mode": "3", "NewMod.Rate": "2.0", "NewMod.Enabled": "false"} {
		got, ok := back.Get(k)
		require.True(t, ok, k)
		require.Equal(t, v, got, k)
	}
	changed, err = back.Set("NewMod.Rate", "2.0")
	require.NoError(t, err)
	require.False(t, changed, "written once, it is edited in place")
}

// A hand edit may drop the ',' Lua needs before an added field.
func TestSandboxModOptionsSeparator(t *testing.T) {
	d := loadSandbox(t, []byte("SandboxVars = {\n    VERSION = 6,\n    Mod = {\n        A = 1\n    }\n}"))
	d.ApplyModOptions([]SandboxOptionDef{
		{Name: "Mod.B", Type: "integer", Min: 0, Max: 9, Default: "0"},
		{Name: "Other.C", Type: "boolean", Default: "false"},
	}, nil)
	_, err := d.Set("Mod.B", "5")
	require.NoError(t, err)
	_, err = d.Set("Other.C", "true")
	require.NoError(t, err)
	require.Equal(t, "SandboxVars = {\n    VERSION = 6,\n    Mod = {\n        A = 1,\n        B = 5,\n    },\n    Other = {\n        C = true,\n    },\n}",
		string(d.Bytes()))
}
