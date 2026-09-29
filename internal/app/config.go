package app

import (
	"errors"
	"fmt"
	"log/slog"
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

type Config struct {
	Env            string        `env:"PANEL_ENV" envDefault:"prod"`
	LogLevel       string        `env:"PANEL_LOG_LEVEL" envDefault:"info"`
	Port           int           `env:"PANEL_PORT" envDefault:"8080"`
	Drivers        DriverMode    `env:"PANEL_DRIVERS" envDefault:"real"`
	Scenario       string        `env:"PANEL_SCENARIO" envDefault:"idle"`
	InstallDir     string        `env:"PANEL_INSTALL_DIR" envDefault:"/project-zomboid"`
	DataDir        string        `env:"PANEL_DATA_DIR" envDefault:"/project-zomboid-config"`
	BackupDir      string        `env:"PANEL_BACKUP_DIR" envDefault:"/backups"`
	DBPath         string        `env:"PANEL_DB_PATH" envDefault:"/project-zomboid-config/pzman.db"`
	AdminUser      string        `env:"PANEL_ADMIN_USER" envDefault:"admin"`
	AdminHash      string        `env:"PANEL_ADMIN_PASSWORD_HASH,required,notEmpty"`
	JWTSecret      string        `env:"PANEL_JWT_SECRET"`
	SessionTTL     time.Duration `env:"PANEL_SESSION_TTL" envDefault:"12h"`
	CookieSecure   CookieSecure  `env:"PANEL_COOKIE_SECURE" envDefault:"auto"`
	TrustedProxies []string      `env:"PANEL_TRUSTED_PROXIES"`
	ConsoleRing    int           `env:"PANEL_CONSOLE_RING" envDefault:"2000"`
	ModsToken      string        `env:"PANEL_MODS_TOKEN"`
	SteamCMD       string        `env:"PANEL_STEAMCMD" envDefault:"/opt/steamcmd/steamcmd.sh"`
	SteamHome      string        `env:"PANEL_STEAM_HOME" envDefault:"/var/lib/pzman/steam"`
	CacheDir       string        `env:"PANEL_CACHE_DIR" envDefault:"/var/lib/pzman/cache"`
	StopTimeout    time.Duration `env:"PANEL_STOP_TIMEOUT" envDefault:"90s"`
	DiskWarnPct    float64       `env:"PANEL_DISK_WARN_PERCENT" envDefault:"85"`
	DiskCritPct    float64       `env:"PANEL_DISK_CRIT_PERCENT" envDefault:"95"`

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
}

// Load reads the environment and validates, reporting every problem at once.
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	var errs []error
	if err != nil {
		var agg env.AggregateError
		if errors.As(err, &agg) {
			errs = append(errs, agg.Errors...)
		} else {
			errs = append(errs, err)
		}
	}
	if verr := cfg.validate(err == nil); verr != nil {
		errs = append(errs, verr)
	}
	return cfg, errors.Join(errs...)
}

// Validate enforces the cross-field rules. Called by Load; exported so tests
// can build a Config literal and check one rule at a time.
func (c Config) Validate() error { return c.validate(true) }

func (c Config) validate(checkHash bool) error {
	var errs []error
	if checkHash || c.AdminHash != "" {
		if err := validateAdminHash(c.AdminHash); err != nil {
			errs = append(errs, err)
		}
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
	return errors.Join(errs...)
}

func validateAdminHash(h string) error {
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

func parseLevel(s string) (slog.Level, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return l, fmt.Errorf("PANEL_LOG_LEVEL %q is not a log level (debug, info, warn, error)", s)
	}
	return l, nil
}

func (c Config) IsDev() bool { return c.Env == "dev" }

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
	return fmt.Sprintf("env=%s port=%d drivers=%s scenario=%s install=%s data=%s backups=%s db=%s admin=%s "+
		"server=%s branch=%q steam=%t puid=%d pgid=%d xmx=%dg tz=%s jwt_secret=%s rcon_password=%s mods_token=%s",
		c.Env, c.Port, c.Drivers, c.Scenario, c.InstallDir, c.DataDir, c.BackupDir, c.DBPath, c.AdminUser,
		c.ServerName, c.ServerBranch, c.UseSteam, c.PUID, c.PGID, c.MemoryXmxGB, c.Timezone,
		setOrUnset(c.JWTSecret), setOrUnset(c.RCONPassword), setOrUnset(c.ModsToken))
}

func setOrUnset(s string) string {
	if s == "" {
		return "unset"
	}
	return "set"
}
