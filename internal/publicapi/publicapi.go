package publicapi

import "time"

// PublicData feeds the unauthenticated mod page, reachable only through the
// unlisted token in its URL.
type PublicData struct {
	ServerName    string          `json:"serverName"`
	Status        string          `json:"status" enum:"available,restarting,unavailable"`
	StatusMessage string          `json:"statusMessage"`
	Players       *int            `json:"players"`
	Connect       *PublicConnect  `json:"connect,omitempty"`
	Launcher      *PublicLauncher `json:"launcher,omitempty" doc:"Launcher release of this server's version; omitted in Steam mode and on unreleased builds"`
	PageURL       string          `json:"pageUrl,omitempty" doc:"This page's address under PANEL_PUBLIC_URL; omitted when unset"`
	GameVersion   string          `json:"gameVersion,omitempty" doc:"Server's game build, e.g. 42.21"`
	Collection    *PublicLink     `json:"collection,omitempty"`
	Items         []PublicItem    `json:"items" nullable:"false"`
	ModsLine      string          `json:"modsLine"`
	MapLine       string          `json:"mapLine"`
	IniName       string          `json:"iniName"`
	Pack          PublicPack      `json:"pack"`
	GeneratedAt   time.Time       `json:"generatedAt"`
}

// PublicConnect is omitted when neither PANEL_PUBLIC_GAME_ADDRESS nor
// PANEL_PUBLIC_URL is set.
type PublicConnect struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// LauncherRepo publishes the launcher builds as assets of every release
// (.github/workflows/launcher.yml).
const LauncherRepo = "magne4000/easy-pz-docker"

// LauncherAsset names a platform's build in a release; macOS has one
// universal build.
func LauncherAsset(goos, goarch string) string {
	switch goos {
	case "windows":
		return "easypz-launcher-windows-" + goarch + ".exe"
	case "darwin":
		return "easypz-launcher-darwin-universal.zip"
	default:
		return "easypz-launcher-" + goos + "-" + goarch
	}
}

// launcherBuilds mirrors the build matrix of .github/workflows/launcher.yml.
var launcherBuilds = []PublicDownload{
	{OS: "windows", Arch: "amd64"},
	{OS: "darwin", Arch: "universal"},
	{OS: "linux", Arch: "amd64"},
	{OS: "linux", Arch: "arm64"},
}

type PublicLauncher struct {
	Version    string           `json:"version"`
	ReleaseURL string           `json:"releaseUrl"`
	Downloads  []PublicDownload `json:"downloads" nullable:"false"`
}

type PublicDownload struct {
	OS   string `json:"os" enum:"windows,darwin,linux"`
	Arch string `json:"arch" enum:"amd64,arm64,universal"`
	URL  string `json:"url"`
}

// NewPublicLauncher links the launcher builds released as version.
func NewPublicLauncher(version string) *PublicLauncher {
	base := "https://github.com/" + LauncherRepo + "/releases/"
	l := &PublicLauncher{Version: version, ReleaseURL: base + "tag/" + version, Downloads: []PublicDownload{}}
	for _, b := range launcherBuilds {
		b.URL = base + "download/" + version + "/" + LauncherAsset(b.OS, b.Arch)
		l.Downloads = append(l.Downloads, b)
	}
	return l
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
	Files       string      `json:"files,omitempty" doc:"Per-file listing of the download (GET, PackFiles) and partial downloads of it (POST FilesRequest, answers a zip); omitted by servers that predate it"`
}

type PublicPack struct {
	URL    string `json:"url"`
	Ready  bool   `json:"ready"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Error  string `json:"error,omitempty"`
}

// PackFiles lists a pack's files so a client can fetch only those that differ.
type PackFiles struct {
	SHA256 string     `json:"sha256" doc:"The pack this listing describes"`
	Files  []PackFile `json:"files"`
}

type PackFile struct {
	Path   string `json:"path" doc:"Slash-separated, starting with the mod folder"`
	Size   int64  `json:"size"`
	Packed int64  `json:"packed" doc:"Compressed size in the pack"`
	SHA256 string `json:"sha256"`
}

// FilesRequest selects files of a PackFiles listing by index; the server
// answers 409 when SHA256 is no longer its current pack.
type FilesRequest struct {
	SHA256 string `json:"sha256"`
	Files  []int  `json:"files"`
}
