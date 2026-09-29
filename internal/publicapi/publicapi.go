package publicapi

import "time"

// PublicData feeds the unauthenticated mod page, reachable only through the
// unlisted token in its URL.
type PublicData struct {
	ServerName    string         `json:"serverName"`
	Status        string         `json:"status" enum:"available,restarting,unavailable"`
	StatusMessage string         `json:"statusMessage"`
	Players       *int           `json:"players"`
	Connect       *PublicConnect `json:"connect,omitempty"`
	GameVersion   string         `json:"gameVersion,omitempty" doc:"Server's game build, e.g. 42.21"`
	Collection    *PublicLink    `json:"collection,omitempty"`
	Items         []PublicItem   `json:"items" nullable:"false"`
	ModsLine      string         `json:"modsLine"`
	MapLine       string         `json:"mapLine"`
	IniName       string         `json:"iniName"`
	Pack          PublicPack     `json:"pack"`
	GeneratedAt   time.Time      `json:"generatedAt"`
}

// PublicConnect is omitted when PANEL_PUBLIC_HOST is unset.
type PublicConnect struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type PublicLink struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type PublicMod struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Folder string `json:"folder"`
}

type PublicItem struct {
	WorkshopID  string      `json:"workshopId"`
	Title       string      `json:"title"`
	URL         string      `json:"url"`
	Size        int64       `json:"size"`
	TimeUpdated time.Time   `json:"timeUpdated,omitzero"`
	Mods        []PublicMod `json:"mods" nullable:"false"`
	Download    PublicPack  `json:"download"`
}

type PublicPack struct {
	URL    string `json:"url"`
	Ready  bool   `json:"ready"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Error  string `json:"error,omitempty"`
}
