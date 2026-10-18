package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

const appName = "linear-tui"

type Config struct {
	DefaultTeam     string              `toml:"default_team"`
	RefreshInterval string              `toml:"refresh_interval"`
	Theme           string              `toml:"theme"`
	Editor          string              `toml:"editor"`
	OAuth           OAuth               `toml:"oauth"`
	Keys            map[string][]string `toml:"keys"`
	List            List                `toml:"list"`
	Git             Git                 `toml:"git"`
}

type OAuth struct {
	ClientID string `toml:"client_id"`
	Port     int    `toml:"port"`
}

type List struct {
	Columns []string `toml:"columns"`
}

type Git struct {
	OpenFromBranch bool `toml:"open_from_branch"`
}

func Default() Config {
	return Config{
		RefreshInterval: "60s",
		Theme:           "auto",
		Git:             Git{OpenFromBranch: true},
	}
}

// Refresh returns the background refresh interval; zero disables it.
func (c Config) Refresh() time.Duration {
	d, err := time.ParseDuration(c.RefreshInterval)
	if err != nil || d < 0 {
		return time.Minute
	}
	return d
}

// EditorCommand returns the editor to use for long-form text.
func (c Config) EditorCommand() string {
	for _, e := range []string{c.Editor, os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if e != "" {
			return e
		}
	}
	return "vi"
}

func (c Config) OAuthClientID() string {
	if id := os.Getenv("LINEAR_OAUTH_CLIENT_ID"); id != "" {
		return id
	}
	return c.OAuth.ClientID
}

func xdg(env string, fallback func() (string, error)) (string, error) {
	if d := os.Getenv(env); d != "" {
		return filepath.Join(d, appName), nil
	}
	d, err := fallback()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, appName), nil
}

// Dir is the configuration directory.
func Dir() (string, error) { return xdg("XDG_CONFIG_HOME", os.UserConfigDir) }

// StateDir holds runtime state such as recent issues.
func StateDir() (string, error) { return xdg("XDG_STATE_HOME", os.UserConfigDir) }

func Path() (string, error) {
	d, err := Dir()
	return filepath.Join(d, "config.toml"), err
}

// Load reads the config file at path, falling back to defaults when the file
// does not exist.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		p, err := Path()
		if err != nil {
			return cfg, err
		}
		path = p
	}
	_, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	return cfg, err
}
