package app

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"golang.org/x/crypto/bcrypt"
)

type DriverMode string // "real" | "fake"

const (
	DriversReal DriverMode = "real"
	DriversFake DriverMode = "fake"
)

type CookieSecure string // "auto" | "true" | "false"

const (
	CookieSecureAuto  CookieSecure = "auto"
	CookieSecureTrue  CookieSecure = "true"
	CookieSecureFalse CookieSecure = "false"
)

type Host string // "docker" | "pelican"

const (
	HostDocker  Host = "docker"
	HostPelican Host = "pelican"
)

type Config struct {
	// Host is where pzman runs: the compose image, or the stock Pelican PZ
	// egg's container (see pelican.go for what changes there).
	Host       Host       `env:"PZMAN_HOST" envDefault:"docker"`
	Env        string     `env:"PANEL_ENV" envDefault:"prod"`
	LogLevel   string     `env:"PANEL_LOG_LEVEL" envDefault:"info"`
	Port       int        `env:"PANEL_PORT" envDefault:"8080"`
	Drivers    DriverMode `env:"PANEL_DRIVERS" envDefault:"real"`
	Scenario   string     `env:"PANEL_SCENARIO" envDefault:"idle"`
	InstallDir string     `env:"PANEL_INSTALL_DIR" envDefault:"/project-zomboid"`
	DataDir    string     `env:"PANEL_DATA_DIR" envDefault:"/project-zomboid-config"`
	BackupDir  string     `env:"PANEL_BACKUP_DIR" envDefault:"/backups"`
	DBPath     string     `env:"PANEL_DB_PATH" envDefault:"/project-zomboid-config/pzman.db"`
	AdminUser  string     `env:"PANEL_ADMIN_USER" envDefault:"admin"`
	// AdminHash is required in Docker mode only: Pelican mode has no
	// password login (Pelican's tokens replace it).
	AdminHash      string        `env:"PANEL_ADMIN_PASSWORD_HASH"`
	JWTSecret      string        `env:"PANEL_JWT_SECRET"`
	SessionTTL     time.Duration `env:"PANEL_SESSION_TTL" envDefault:"12h"`
	CookieSecure   CookieSecure  `env:"PANEL_COOKIE_SECURE" envDefault:"auto"`
	TrustedProxies []string      `env:"PANEL_TRUSTED_PROXIES"`
	ConsoleRing    int           `env:"PANEL_CONSOLE_RING" envDefault:"2000"`
	ModsToken      string        `env:"PANEL_MODS_TOKEN"`
	PublicURL      string        `env:"PANEL_PUBLIC_URL"`
	// PublicGameAddress: set when players reach the game elsewhere than
	// PublicURL's host (CDN or HTTP-only proxy, another machine, other port).
	PublicGameAddress string `env:"PANEL_PUBLIC_GAME_ADDRESS"`
	SteamCMD          string `env:"PANEL_STEAMCMD" envDefault:"/opt/steamcmd/steamcmd.sh"`
	SteamHome         string `env:"PANEL_STEAM_HOME" envDefault:"/var/lib/pzman/steam"`
	CacheDir          string `env:"PANEL_CACHE_DIR" envDefault:"/var/lib/pzman/cache"`
	// LogDir receives pzman's JSON log file in Pelican mode (stdout is the
	// panel console there); unused in Docker mode, which logs to stdout.
	LogDir        string        `env:"PANEL_LOG_DIR"`
	SteamPlatform string        `env:"STEAM_PLATFORM"`
	StopTimeout   time.Duration `env:"PANEL_STOP_TIMEOUT" envDefault:"90s"`
	DiskWarnPct   float64       `env:"PANEL_DISK_WARN_PERCENT" envDefault:"85"`
	DiskCritPct   float64       `env:"PANEL_DISK_CRIT_PERCENT" envDefault:"95"`

	// Defaults for settings the UI can override (stored in the DB).
	BackupInterval      time.Duration `env:"PANEL_BACKUP_INTERVAL" envDefault:"2h"`
	BackupKeep          int           `env:"PANEL_BACKUP_KEEP" envDefault:"24"`
	BackupKeepDaily     int           `env:"PANEL_BACKUP_KEEP_DAILY" envDefault:"7"`
	BackupKeepWeekly    int           `env:"PANEL_BACKUP_KEEP_WEEKLY" envDefault:"4"`
	BackupMaxTotalGB    float64       `env:"PANEL_BACKUP_MAX_TOTAL_GB" envDefault:"0"`
	UpdateCheckInterval time.Duration `env:"PANEL_UPDATE_CHECK_INTERVAL" envDefault:"1h"`
	UpdateMaxDelay      time.Duration `env:"PANEL_UPDATE_MAX_DELAY" envDefault:"2h"`
	WorkshopCollection  string        `env:"PANEL_WORKSHOP_COLLECTION"`

	// Inherited from the broccoli image for drop-in compatibility. These keep
	// their legacy unprefixed names on purpose: renaming them
	// would break existing compose files for no gain.
	PUID              int    `env:"PUID" envDefault:"568"`
	PGID              int    `env:"PGID" envDefault:"568"`
	ServerName        string `env:"SERVER_NAME" envDefault:"pzserver"`
	DefaultPort       int    `env:"DEFAULT_PORT" envDefault:"16261"`
	UDPPort           int    `env:"UDP_PORT" envDefault:"16262"`
	MaxPlayers        int    `env:"MAX_PLAYERS" envDefault:"32"`
	VMArgs            string `env:"VM_ARGS"`
	ServerBranch      string `env:"SERVER_BRANCH"`
	MemoryXmxGB       int    `env:"MEMORY_XMX_GB" envDefault:"8"`
	RCONPort          int    `env:"RCON_PORT" envDefault:"27015"`
	RCONPassword      string `env:"RCON_PASSWORD"`
	GameAdminUser     string `env:"ADMIN_USERNAME" envDefault:"admin"`
	GameAdminPassword string `env:"ADMIN_PASSWORD"`
	ServerPassword    string `env:"SERVER_PASSWORD"`
	UseSteam          bool   `env:"USE_STEAM" envDefault:"true"`
	SteamVAC          string `env:"STEAM_VAC"`
	UpdateOnStart     bool   `env:"UPDATE_ON_START" envDefault:"true"`
	Timezone          string `env:"TZ" envDefault:"UTC"`
	// EggAutoUpdate is the Pelican egg's AUTO_UPDATE variable, read only to
	// warn when the yolk image updates the game behind pzman's back.
	EggAutoUpdate string `env:"AUTO_UPDATE"`

	// Pelican mode: the plugin's settings (pzman.env contract). The public
	// key verifies Pelican's tokens and proxy signatures; the plugin keeps the
	// private key, so reading pzman.env does not allow minting tokens.
	PelicanServer    string `env:"PZMAN_PELICAN_SERVER"`
	PelicanPublicKey string `env:"PZMAN_PELICAN_PUBLIC_KEY"`
	PelicanPanelURL  string `env:"PZMAN_PELICAN_PANEL_URL"`

	// Root is the Pelican server folder (/home/container), the parent of the
	// executable's directory; empty in Docker mode.
	Root string
}

// Load reads pzman.env (next to the executable) and the environment, applies
// the Pelican-mode defaults and egg aliases, and validates, reporting every
// problem at once with where its value came from.
func Load() (Config, error) {
	exe, err := os.Executable()
	if err != nil {
		return Config{}, fmt.Errorf("locate the executable: %w", err)
	}
	return load(os.Environ(), exe)
}

func load(environ []string, exe string) (Config, error) {
	vars, src, gerr := gatherVars(environ, exe)
	if vars == nil {
		return Config{}, gerr
	}
	cfg, err := env.ParseAsWithOptions[Config](env.Options{Environment: vars})
	if Host(vars["PZMAN_HOST"]) == HostPelican {
		cfg.Root = pelicanRoot(exe)
	}
	var errs []error
	if gerr != nil {
		errs = append(errs, gerr)
	}
	if err != nil {
		var agg env.AggregateError
		if errors.As(err, &agg) {
			errs = append(errs, agg.Errors...)
		} else {
			errs = append(errs, err)
		}
	}
	if verr := cfg.Validate(); verr != nil {
		errs = append(errs, verr)
	}
	return cfg, src.annotate(errors.Join(errs...))
}

// Validate enforces the cross-field rules. Called by Load; exported so tests
// can build a Config literal and check one rule at a time.
func (c Config) Validate() error {
	var errs []error
	switch c.Host {
	case "", HostDocker, HostPelican: // "" is a Config literal: Docker mode
	default:
		errs = append(errs, fmt.Errorf("PZMAN_HOST must be %q or %q, got %q", HostDocker, HostPelican, c.Host))
	}
	if c.Pelican() {
		errs = append(errs, c.validatePelican()...)
	} else if err := validateAdminHash(c.AdminHash); err != nil {
		errs = append(errs, err)
	}
	switch c.Drivers {
	case DriversReal, DriversFake:
	default:
		errs = append(errs, fmt.Errorf("PANEL_DRIVERS must be %q or %q, got %q", DriversReal, DriversFake, c.Drivers))
	}
	switch c.CookieSecure {
	case CookieSecureAuto, CookieSecureTrue, CookieSecureFalse:
	default:
		errs = append(errs, fmt.Errorf("PANEL_COOKIE_SECURE must be auto, true or false, got %q", c.CookieSecure))
	}
	if _, err := parseLevel(c.LogLevel); err != nil {
		errs = append(errs, err)
	}
	if c.Drivers == DriversReal && c.Scenario != "" && c.Scenario != "idle" {
		errs = append(errs, fmt.Errorf("PANEL_SCENARIO=%q requires PANEL_DRIVERS=fake", c.Scenario))
	}
	if c.Port <= 0 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("PANEL_PORT %d is out of range", c.Port))
	}
	if c.ConsoleRing <= 0 {
		errs = append(errs, errors.New("PANEL_CONSOLE_RING must be positive"))
	}
	if c.ServerName == "" || strings.ContainsAny(c.ServerName, `/\`) {
		errs = append(errs, fmt.Errorf("SERVER_NAME %q is not a valid server name", c.ServerName))
	}
	if c.PublicURL != "" {
		if err := validatePublicURL(c.PublicURL, c.Pelican()); err != nil {
			errs = append(errs, err)
		}
	}
	if c.PublicGameAddress != "" {
		if _, _, ok := parseHostPort(c.PublicGameAddress); !ok {
			errs = append(errs, fmt.Errorf("PANEL_PUBLIC_GAME_ADDRESS must be host[:port] (IPv6 in brackets), got %q",
				c.PublicGameAddress))
		}
	}
	return errors.Join(errs...)
}

var (
	uuidRe       = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	publicKeyRe  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	publicPathRe = regexp.MustCompile(`^(/[A-Za-z0-9._~%-]+)*/?$`)
)

// validatePelican checks the plugin's settings (pzman.env contract).
func (c Config) validatePelican() []error {
	var errs []error
	switch {
	case c.PelicanServer == "":
		errs = append(errs, errors.New("PZMAN_PELICAN_SERVER is required in Pelican mode (the server's UUID)"))
	case !uuidRe.MatchString(c.PelicanServer):
		errs = append(errs, fmt.Errorf("PZMAN_PELICAN_SERVER must be the server's UUID, got %q", c.PelicanServer))
	}
	switch {
	case c.PelicanPublicKey == "":
		errs = append(errs, errors.New("PZMAN_PELICAN_PUBLIC_KEY is required in Pelican mode (the plugin's Ed25519 public key)"))
	case !publicKeyRe.MatchString(c.PelicanPublicKey):
		errs = append(errs, fmt.Errorf("PZMAN_PELICAN_PUBLIC_KEY must be an Ed25519 public key as 64 lowercase hex characters, got %q",
			c.PelicanPublicKey))
	}
	if c.PelicanPanelURL == "" {
		errs = append(errs, errors.New("PZMAN_PELICAN_PANEL_URL is required in Pelican mode (the panel's URL)"))
	} else if u, ok := parseHTTPURL(c.PelicanPanelURL); !ok || !publicPathRe.MatchString(u.EscapedPath()) {
		errs = append(errs, fmt.Errorf("PZMAN_PELICAN_PANEL_URL must be http(s)://host[:port][/path], got %q", c.PelicanPanelURL))
	}
	return errs
}

func validateAdminHash(h string) error {
	if h == "" {
		return errors.New("PANEL_ADMIN_PASSWORD_HASH is required (a bcrypt hash); see the README for how to generate one")
	}
	// bcrypt only. A non-empty check is NOT enough: a plaintext password
	// here would boot fine and then reject every login forever.
	if len(h) < 59 || (!strings.HasPrefix(h, "$2a$") &&
		!strings.HasPrefix(h, "$2b$") && !strings.HasPrefix(h, "$2y$")) {
		return errors.New("PANEL_ADMIN_PASSWORD_HASH is not a bcrypt hash " +
			"($2a$/$2b$/$2y$); see the README for how to generate one")
	}
	cost, err := bcrypt.Cost([]byte(h))
	if err != nil {
		return fmt.Errorf("PANEL_ADMIN_PASSWORD_HASH is malformed: %w", err)
	}
	if cost < 10 {
		return fmt.Errorf("PANEL_ADMIN_PASSWORD_HASH cost %d is too low, use 12", cost)
	}
	return nil
}

// parseHTTPURL accepts http(s)://host[:port] with any path, without
// credentials, query or fragment.
func parseHTTPURL(s string) (*url.URL, bool) {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" ||
		u.ForceQuery || u.Fragment != "" {
		return nil, false
	}
	if _, _, ok := parseHostPort(u.Host); !ok {
		return nil, false
	}
	return u, true
}

// validatePublicURL accepts an origin in Docker mode, where the panel is
// served from the root; in Pelican mode the panel's public routes add a path
// (<panel>/easy-pz/<uuid_short>).
func validatePublicURL(s string, withPath bool) error {
	u, ok := parseHTTPURL(s)
	if withPath {
		if !ok || !publicPathRe.MatchString(u.EscapedPath()) {
			return fmt.Errorf("PANEL_PUBLIC_URL must be http(s)://host[:port][/path], got %q", s)
		}
		return nil
	}
	if !ok || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("PANEL_PUBLIC_URL must be http(s)://host[:port], got %q", s)
	}
	return nil
}

// parseHostPort reads "host", "host:port" or "[ipv6]:port"; port 0 when absent.
func parseHostPort(s string) (host string, port int, ok bool) {
	if !strings.HasPrefix(s, "[") && strings.Count(s, ":") > 1 {
		return "", 0, false // unbracketed IPv6 would parse as host "2001:db8:", port 1
	}
	u, err := url.Parse("//" + s)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", 0, false
	}
	if p := u.Port(); p != "" {
		if port, err = strconv.Atoi(p); err != nil || port < 1 || port > 65535 {
			return "", 0, false
		}
	}
	return u.Hostname(), port, true
}

// PublicBase is PANEL_PUBLIC_URL without its trailing slash, "" when unset:
// the public mod page is <PublicBase>/mods/<token>/.
func (c Config) PublicBase() string { return strings.TrimSuffix(c.PublicURL, "/") }

// PublicPath is the path of PublicBase ("" in Docker mode, where it is an
// origin; /easy-pz/<uuid_short> behind Pelican), prefixed to the public
// URLs pzman builds so they resolve through the panel's proxy.
func (c Config) PublicPath() string {
	u, err := url.Parse(c.PublicBase())
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(u.EscapedPath(), "/")
}

// GameAddress is where the launcher joins: PANEL_PUBLIC_GAME_ADDRESS, else
// PANEL_PUBLIC_URL's hostname (that URL's port is the panel's). Port 0 means
// the server's DefaultPort.
func (c Config) GameAddress() (host string, port int) {
	if c.PublicGameAddress != "" {
		host, port, _ = parseHostPort(c.PublicGameAddress)
		return host, port
	}
	if u, err := url.Parse(c.PublicURL); err == nil {
		return u.Hostname(), 0
	}
	return "", 0
}

func parseLevel(s string) (slog.Level, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return l, fmt.Errorf("PANEL_LOG_LEVEL %q is not a log level (debug, info, warn, error)", s)
	}
	return l, nil
}

func (c Config) IsDev() bool { return c.Env == "dev" }

// Pelican reports Pelican mode (PZMAN_HOST=pelican).
func (c Config) Pelican() bool { return c.Host == HostPelican }

// EggAutoUpdateOn is the egg's AUTO_UPDATE=1 in Pelican mode: the yolk image
// then runs SteamCMD at every container start, before pzman.
func (c Config) EggAutoUpdateOn() bool { return c.Pelican() && c.EggAutoUpdate == "1" }

func (c Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		// e.g. a POSIX TZ string like "CEST-2", which Go cannot load by name.
		return time.Local
	}
	return loc
}

// Redacted is a one-line, log-safe rendering: never the hash, secret or token.
func (c Config) Redacted() string {
	return fmt.Sprintf("host=%s env=%s port=%d drivers=%s scenario=%s install=%s data=%s backups=%s db=%s admin=%s "+
		"server=%s branch=%q steam=%t puid=%d pgid=%d xmx=%dg game_port=%d udp_port=%d tz=%s public_url=%q "+
		"public_game_address=%q pelican_server=%q pelican_public_key=%q pelican_panel_url=%q jwt_secret=%s rcon_password=%s "+
		"mods_token=%s",
		c.Host, c.Env, c.Port, c.Drivers, c.Scenario, c.InstallDir, c.DataDir, c.BackupDir, c.DBPath, c.AdminUser,
		c.ServerName, c.ServerBranch, c.UseSteam, c.PUID, c.PGID, c.MemoryXmxGB, c.DefaultPort, c.UDPPort, c.Timezone,
		c.PublicURL, c.PublicGameAddress, c.PelicanServer, c.PelicanPublicKey, c.PelicanPanelURL, setOrUnset(c.JWTSecret),
		setOrUnset(c.RCONPassword), setOrUnset(c.ModsToken))
}

func setOrUnset(s string) string {
	if s == "" {
		return "unset"
	}
	return "set"
}
