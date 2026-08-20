// Package config loads Trigpoint's settings. A missing config file is not an
// error: the documented defaults are enough to run, so a first launch needs no
// setup, and a file that exists overrides only the keys it actually sets.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// PreviewLines is how many lines of output each card size shows. Small shows
// none, which is also how a node opts out of being captured at all.
type PreviewLines struct {
	S int `toml:"s"`
	M int `toml:"m"`
	L int `toml:"l"`
}

type General struct {
	DefaultWorkspace    string       `toml:"default_workspace"`
	PreviewLines        PreviewLines `toml:"preview_lines"`
	PreviewDebounceMs   int          `toml:"preview_debounce_ms"`
	RefreshTickS        int          `toml:"refresh_tick_s"`
	StatusStaleAfterMin int          `toml:"status_stale_after_min"`
	ConfirmQuit         bool         `toml:"confirm_quit"`
	// BellOnNeedsYou rings the terminal when an agent starts needing you (§8).
	// Off unless asked for: a map is a thing you glance at, and a tool that
	// beeps without being asked is one that gets muted.
	BellOnNeedsYou bool `toml:"bell_on_needs_you"`
	// DetachKey is the binding Trigpoint installs into tmux so that a single
	// keystroke returns from an attached node to the map.
	DetachKey string `toml:"detach_key"`
}

// Agent is a named command line offered when creating an agent node. Adding a
// preset is an edit to config, never a code change.
type Agent struct {
	Cmd string `toml:"cmd"`
}

type Config struct {
	General General           `toml:"general"`
	Agents  map[string]Agent  `toml:"agents"`
	Keymap  map[string]string `toml:"keymap"`
}

// Default is the configuration Trigpoint runs with when no file exists.
func Default() Config {
	return Config{
		General: General{
			DefaultWorkspace:    "main",
			PreviewLines:        PreviewLines{S: 0, M: 4, L: 10},
			PreviewDebounceMs:   500,
			RefreshTickS:        5,
			StatusStaleAfterMin: 10,
			ConfirmQuit:         false,
			BellOnNeedsYou:      false,
			DetachKey:           "M-Escape",
		},
		Agents: map[string]Agent{
			"claude": {Cmd: "claude"},
			"codex":  {Cmd: "codex"},
		},
		Keymap: map[string]string{},
	}
}

// Path is where Trigpoint looks for its config file. XDG_CONFIG_HOME is honoured
// by hand rather than through os.UserConfigDir, which resolves to Application
// Support on macOS — SPEC §9.1 documents ~/.config/trig/config.toml on every
// supported platform, and state.Dir resolves the same way.
func Path() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locating the config directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "trig", "config.toml"), nil
}

// Load reads the config file at path, layering it over the defaults so a partial
// file keeps every default it does not mention. A file that is not there is fine;
// a file that is there and malformed is not, because silently ignoring it would
// leave the user with settings they cannot see.
func Load(path string) (Config, error) {
	cfg := Default()
	// Emptied first, so that an agents table in the file is the list of presets
	// rather than an addition to a built-in one: a decode merges into a map it
	// finds, and a preset that ships by default could then never be removed —
	// choosing one for an agent that is not installed makes a node whose
	// session runs "command not found".
	cfg.Agents = nil
	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Default(), nil
		}
		return Config{}, fmt.Errorf("reading config %s: %w", path, err)
	}
	// A key Trigpoint does not understand is almost always a typo, and silently
	// dropping it leaves the user with a setting they can see in the file and
	// cannot see taking effect.
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return Config{}, fmt.Errorf("reading config %s: unknown %s: %s",
			path, plural(len(keys), "key"), strings.Join(keys, ", "))
	}
	if cfg.Agents == nil {
		cfg.Agents = Default().Agents
	}
	return cfg, nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}
