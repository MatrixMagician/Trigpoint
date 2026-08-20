// Package hooks installs the agent-side plumbing that makes an agent report its
// own status (SPEC §8). It is the one place in Trigpoint that knows what an
// agent's configuration file looks like.
//
// That confinement is the point. The status file format is the contract and is
// expected to outlive every agent release; hook configuration is expected to
// change under it (SPEC §14, risk 3). Nothing outside this package reads or
// writes Claude Code's settings, so when the format moves, one file moves with
// it. See docs/adr/0016-agent-hooks-are-installed-explicitly-and-merged.md.
package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MatrixMagician/Trigpoint/internal/status"
)

// Entry is one hook Trigpoint installs: the agent event it fires on, and the
// command that event runs.
type Entry struct {
	// Event is Claude Code's name for the moment the hook fires.
	Event string
	// State is what the node reports when it does. It is also how an installed
	// entry is recognised: the command has to invoke `trig emit-status` with
	// this state, whatever else the user has wrapped around it.
	State status.State
	// Command is what gets written into the settings file.
	Command string
	// Why is one line for the user, printed beside the entry when it is added.
	Why string
}

// Entries is the whole installation. Three events, one per state a hook can
// honestly know about: an agent cannot report `error` from a hook, because a
// hook that fires is a hook whose session is still working.
//
// Each command is guarded on TRIG_STATUS_FILE because these hooks fire in every
// Claude Code session on the machine, not only in the ones running inside a
// node. Outside one there is nothing to report to, and the honest entry is a
// silent no-op rather than an error the user is shown on every prompt.
var Entries = []Entry{
	{Event: "UserPromptSubmit", State: status.Running, Command: guard(status.Running), Why: "you gave it something to do"},
	{Event: "Notification", State: status.NeedsYou, Command: guard(status.NeedsYou, notASleepingPrompt), Why: "it is asking you something"},
	{Event: "Stop", State: status.Done, Command: guard(status.Done), Why: "it has finished answering"},
}

// notASleepingPrompt is the extra clause on the Notification hook.
//
// Claude Code fires Notification for two different things: a permission
// request, which is needs-you, and the prompt having sat idle for a minute,
// which is not. Unfiltered, every node that reported done would turn amber a
// minute later, ring the bell, and pull `u` to itself — and grey would be a
// state no card could ever be seen in.
//
// The payload arrives on stdin, and this reads the one field that tells the two
// apart. Matching Claude Code's own wording is the brittle part; the worst a
// change to it can do is let an idle notification through as needs-you, which is
// where this started rather than a broken hook.
const notASleepingPrompt = `grep -q "waiting for your input"`

// guard builds one entry's command: the emit, behind a test for the status file
// and behind any further reason this particular event has for staying quiet.
//
// Every clause is a `||`, so each one that holds ends the command at exit 0.
// The status file test comes first and is on every entry, because these hooks
// fire in every Claude Code session on the machine and outside a node there is
// nothing to report to — a failure there would be an error shown on every
// prompt to a user who has done nothing wrong.
func guard(s status.State, unless ...string) string {
	clauses := append([]string{`[ -z "$TRIG_STATUS_FILE" ]`}, unless...)
	return strings.Join(clauses, " || ") + " || " + fmt.Sprintf("trig emit-status %s", s)
}

// SettingsPath is where Claude Code keeps the user's settings. CLAUDE_CONFIG_DIR
// is honoured because a user who has moved that directory has moved the file
// this command is supposed to be merging into.
func SettingsPath() (string, error) {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locating Claude Code's settings: %w", err)
		}
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, "settings.json"), nil
}

// Status is the entries that are not installed at path. Nothing missing is a
// working installation; everything missing is a user who has not run the
// command, which is not a fault. Anything in between is drift, and is what
// `trig doctor` exists to catch.
//
// A settings file that is not there is everything missing rather than an error:
// a machine with no Claude Code configuration has no hooks installed.
func Status(path string) ([]Entry, error) {
	settings, err := load(path)
	if err != nil {
		return nil, err
	}
	return missing(settings)
}

// Install merges the missing entries into path and returns the ones it added —
// empty when there was nothing to do, which is what running it twice looks
// like. A dry run reports the same and writes nothing.
//
// Everything else in the file survives: unrelated settings, and hooks belonging
// to somebody else on the same events. A file that does not parse is refused
// rather than replaced by one Trigpoint invented.
func Install(path string, dryRun bool) ([]Entry, error) {
	settings, err := load(path)
	if err != nil {
		return nil, err
	}
	todo, err := missing(settings)
	if err != nil {
		return nil, err
	}
	if len(todo) == 0 || dryRun {
		return todo, nil
	}

	if settings == nil {
		settings = map[string]any{}
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}
	for _, e := range todo {
		groups, _ := hooks[e.Event].([]any)
		hooks[e.Event] = append(groups, map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": e.Command}},
		})
	}
	if err := save(path, settings); err != nil {
		return nil, err
	}
	return todo, nil
}

// load reads the settings file. A file that is not there is no settings, so
// that installing into a machine that has never been configured writes a fresh
// file rather than complaining about a missing one.
func load(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (%w); Trigpoint will not rewrite a file it cannot read", path, err)
	}
	return settings, nil
}

// save replaces the settings file atomically, because it is a file another
// program reads and a half-written settings file is one Claude Code refuses to
// start with. Written 0o600 like the rest of a user's agent configuration.
func save(path string, settings map[string]any) error {
	raw, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the settings: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating a temp file beside %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) // no-op once the rename has succeeded

	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("setting the mode on %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// missing is the entries with no command installed against their event. An
// entry counts as installed when some command on its event invokes
// `trig emit-status` with its state — a user who has wrapped the emit in their
// own shell has installed it, and re-running must not staple a second copy
// underneath.
func missing(settings map[string]any) ([]Entry, error) {
	var todo []Entry
	for _, e := range Entries {
		cmds, err := commands(settings, e.Event)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(cmds, func(c string) bool {
			return strings.Contains(c, "emit-status "+string(e.State))
		}) {
			todo = append(todo, e)
		}
	}
	return todo, nil
}

// commands is every command configured on one event. A shape that is not the
// documented one is an error rather than an empty list: merging into a file
// Trigpoint has misread is how unrelated configuration gets lost.
func commands(settings map[string]any, event string) ([]string, error) {
	if settings == nil {
		return nil, nil
	}
	rawHooks, ok := settings["hooks"]
	if !ok || rawHooks == nil {
		return nil, nil
	}
	hooks, ok := rawHooks.(map[string]any)
	if !ok {
		return nil, errors.New(`"hooks" is not an object`)
	}
	rawGroups, ok := hooks[event]
	if !ok || rawGroups == nil {
		return nil, nil
	}
	groups, ok := rawGroups.([]any)
	if !ok {
		return nil, fmt.Errorf("hooks.%s is not a list", event)
	}
	var found []string
	for i, rawGroup := range groups {
		group, ok := rawGroup.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("hooks.%s[%d] is not an object", event, i)
		}
		rawEntries, ok := group["hooks"]
		if !ok || rawEntries == nil {
			continue
		}
		entries, ok := rawEntries.([]any)
		if !ok {
			return nil, fmt.Errorf("hooks.%s[%d].hooks is not a list", event, i)
		}
		for j, rawEntry := range entries {
			entry, ok := rawEntry.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("hooks.%s[%d].hooks[%d] is not an object", event, i, j)
			}
			if command, ok := entry["command"].(string); ok {
				found = append(found, command)
			}
		}
	}
	return found, nil
}
