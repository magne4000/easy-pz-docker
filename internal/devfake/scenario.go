// Package devfake provides in-memory drivers and named scenario timelines so
// every UI state is reachable on a laptop without PZ, SteamCMD or RCON.
package devfake

import (
	"fmt"
	"sort"
	"time"
)

type Scenario struct {
	Name              string
	Description       string
	InstalledBuild    string
	LatestBuild       string
	Players           []string
	PlayersLeaveAfter time.Duration
	CrashAfter        time.Duration
	SlowBackup        bool
	TriggerBackup     bool
	Tracked           []string
	OutdatedMods      []string
}

const (
	buildOld = "24909836"
	buildNew = "25014412"
)

var scenarios = map[string]Scenario{
	"idle": {
		Description:    "Installed and up to date, two mods, nobody online",
		InstalledBuild: buildNew, LatestBuild: buildNew,
		Tracked: []string{"2392709985", "2169435993"},
	},
	"update-window": {
		Description:    "Game and one mod outdated; two players online who leave after 90s",
		InstalledBuild: buildOld, LatestBuild: buildNew,
		Players: []string{"alice", "bob"}, PlayersLeaveAfter: 90 * time.Second,
		Tracked: []string{"2392709985", "2478768005", "2200148440"}, OutdatedMods: []string{"2478768005"},
	},
	"backup-running": {
		Description:    "A slow backup starts right after boot",
		InstalledBuild: buildNew, LatestBuild: buildNew, SlowBackup: true, TriggerBackup: true,
		Players: []string{"carol"}, Tracked: []string{"2392709985"},
	},
	"crash-loop": {
		Description:    "The server crashes shortly after every start",
		InstalledBuild: buildNew, LatestBuild: buildNew, CrashAfter: 8 * time.Second,
		Tracked: []string{"2392709985"},
	},
	"mod-conflict": {
		Description:    "Two enabled mods override the same files; one mod misses a requirement",
		InstalledBuild: buildNew, LatestBuild: buildNew,
		Tracked: []string{"2875848298", "2875848299", "2478768005"},
	},
}

func Lookup(name string) (Scenario, error) {
	sc, ok := scenarios[name]
	if !ok {
		return Scenario{}, fmt.Errorf("unknown PANEL_SCENARIO %q (known: %v)", name, Names())
	}
	sc.Name = name
	return sc, nil
}

func Names() []string {
	out := make([]string, 0, len(scenarios))
	for n := range scenarios {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

type catalogMod struct {
	id, name string
	requires []string
	maps     []string
	files    []string
}

type catalogItem struct {
	title   string
	size    int64
	updated time.Time
	mods    []catalogMod
}

var catalog = map[string]catalogItem{
	"2392709985": {title: "Tsar's Common Library", size: 18_400_000, updated: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
		mods: []catalogMod{{id: "tsarslib", name: "Tsar's Common Library", files: []string{"media/lua/shared/TCL/Core.lua"}}}},
	"2169435993": {title: "Mod Manager", size: 31_729, updated: time.Date(2026, 5, 11, 8, 30, 0, 0, time.UTC),
		mods: []catalogMod{{id: "modmanager", name: "Mod Manager", files: []string{"media/lua/client/ModManager.lua"}}}},
	"2478768005": {title: "Brita's Weapon Pack", size: 3_088_000_000, updated: time.Date(2026, 9, 20, 18, 0, 0, 0, time.UTC),
		mods: []catalogMod{
			{id: "Brita", name: "Brita's Weapon Pack", requires: []string{"tsarslib", "Arsenal(26)GunFighter"}, files: []string{"media/scripts/Brita_Weapons.txt", "media/lua/shared/Brita/Main.lua"}},
			{id: "Brita_2", name: "Brita's Weapon Pack (Launchers)", requires: []string{"Brita"}, files: []string{"media/scripts/Brita_Launchers.txt"}},
		}},
	"2200148440": {title: "Raven Creek", size: 412_000_000, updated: time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC),
		mods: []catalogMod{{id: "RavenCreek", name: "Raven Creek", maps: []string{"RavenCreek"}, files: []string{"media/maps/RavenCreek/map.info"}}}},
	"2875848298": {title: "Better Loot A", size: 120_000, updated: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC),
		mods: []catalogMod{{id: "BetterLootA", name: "Better Loot A", files: []string{"media/lua/server/Items/Distributions.lua", "media/lua/shared/LootA.lua"}}}},
	"2875848299": {title: "Better Loot B", size: 98_000, updated: time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC),
		mods: []catalogMod{{id: "BetterLootB", name: "Better Loot B", files: []string{"media/lua/server/Items/Distributions.lua"}}}},
}

var collections = map[string][]string{
	"3000000000": {"2392709985", "2169435993", "2200148440"},
}

func item(id string) catalogItem {
	if it, ok := catalog[id]; ok {
		return it
	}
	return catalogItem{title: "Workshop item " + id, size: 250_000, updated: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		mods: []catalogMod{{id: "Mod" + id, name: "Mod " + id, files: []string{"media/lua/shared/Mod" + id + ".lua"}}}}
}
