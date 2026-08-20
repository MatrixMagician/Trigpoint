package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("a missing config file must not be an error: %v", err)
	}
	if diff := diffAgainstDefault(got); diff != "" {
		t.Errorf("missing file should give documented defaults; %s", diff)
	}
}

func TestLoadPartialFileOverridesOnlyWhatItSets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "[general]\nconfirm_quit = true\nrefresh_tick_s = 30\n")

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.General.ConfirmQuit {
		t.Error("confirm_quit = true was not applied")
	}
	if got.General.RefreshTickS != 30 {
		t.Errorf("refresh_tick_s = 30 was not applied, got %d", got.General.RefreshTickS)
	}
	if got.General.DefaultWorkspace != Default().General.DefaultWorkspace {
		t.Errorf("unset default_workspace should keep its default, got %q", got.General.DefaultWorkspace)
	}
	if got.General.DetachKey != Default().General.DetachKey {
		t.Errorf("unset detach_key should keep its default, got %q", got.General.DetachKey)
	}
	if got.General.PreviewLines != Default().General.PreviewLines {
		t.Errorf("unset preview_lines should keep its defaults, got %+v", got.General.PreviewLines)
	}
}

func TestLoadRejectsMalformedTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "[general\nconfirm_quit = yes")

	if _, err := Load(path); err == nil {
		t.Fatal("malformed TOML must be reported, not silently ignored")
	}
}

func TestDefaultShipsClaudeAndCodexPresets(t *testing.T) {
	agents := Default().Agents
	for _, name := range []string{"claude", "codex"} {
		if agents[name].Cmd == "" {
			t.Errorf("preset %q missing a command", name)
		}
	}
}

func TestDefaultPreviewLinesMatchCardSizes(t *testing.T) {
	pl := Default().General.PreviewLines
	if pl.S != 0 || pl.M != 4 || pl.L != 10 {
		t.Errorf("preview line counts should be 0/4/10 for S/M/L, got %+v", pl)
	}
}

func diffAgainstDefault(got Config) string {
	want := Default()
	if got.General != want.General {
		return "general section differs"
	}
	if len(got.Agents) != len(want.Agents) {
		return "agent presets differ"
	}
	return ""
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	cases := map[string]string{
		"misspelled key":     "[general]\nrefresh_ticks_s = 30\n",
		"unknown section":    "[genrl]\nconfirm_quit = true\n",
		"nested agent table": "[agents.custom.example]\ncmd = \"aider --model x\"\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			write(t, path, body)

			_, err := Load(path)
			if err == nil {
				t.Fatal("a key Trigpoint does not understand must be reported, not silently dropped")
			}
			if !strings.Contains(err.Error(), "unknown") {
				t.Errorf("the error should say the key is unknown, got: %v", err)
			}
		})
	}
}

func TestPathHonoursXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/custom/config", "trig", "config.toml"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

// SPEC §9.1 documents ~/.config/trig/config.toml on every supported platform, so
// this must not follow os.UserConfigDir onto macOS's Application Support.
func TestPathFallsBackToDotConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/someone")
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/home/someone", ".config", "trig", "config.toml"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestAnAgentsTableReplacesThePresets(t *testing.T) {
	// The presets are the ones config offers and no others: a file that names
	// its own is a list, not an addition, or a preset shipped by default could
	// never be got rid of — and choosing one for an agent that is not installed
	// makes a node whose session runs "command not found".
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "[agents.aider]\ncmd = \"aider\"\n")

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Agents) != 1 || got.Agents["aider"].Cmd != "aider" {
		t.Errorf("agents = %+v, want only the configured one", got.Agents)
	}
}

func TestNoAgentsTableKeepsTheDefaultPresets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "[general]\nconfirm_quit = true\n")

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Agents) != len(Default().Agents) {
		t.Errorf("agents = %+v, want the defaults", got.Agents)
	}
}
