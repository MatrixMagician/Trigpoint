package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var errExit = errors.New("exit status 1")

func TestParseTmuxVersion(t *testing.T) {
	cases := []struct {
		out          string
		major, minor int
		wantErr      bool
	}{
		{out: "tmux 3.7b\n", major: 3, minor: 7},
		{out: "tmux 3.2a", major: 3, minor: 2},
		{out: "tmux 3.2", major: 3, minor: 2},
		{out: "tmux 2.9a", major: 2, minor: 9},
		{out: "tmux next-3.4", major: 3, minor: 4},
		{out: "tmux 3.1c\n", major: 3, minor: 1},
		{out: "tmux master", wantErr: true},
		{out: "", wantErr: true},
		{out: "not tmux at all", wantErr: true},
	}
	for _, c := range cases {
		t.Run(strings.TrimSpace(c.out), func(t *testing.T) {
			got, err := parseTmuxVersion(c.out)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q, got %v", c.out, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTmuxVersion(%q): %v", c.out, err)
			}
			if got.Major != c.major || got.Minor != c.minor {
				t.Errorf("got %d.%d, want %d.%d", got.Major, got.Minor, c.major, c.minor)
			}
		})
	}
}

func TestVersionAtLeastGatesOnThreeTwo(t *testing.T) {
	cases := []struct {
		v  Version
		ok bool
	}{
		{Version{3, 2}, true},
		{Version{3, 7}, true},
		{Version{4, 0}, true},
		{Version{3, 1}, false},
		{Version{2, 9}, false},
		{Version{1, 8}, false},
	}
	for _, c := range cases {
		if got := c.v.AtLeast(minTmux); got != c.ok {
			t.Errorf("Version{%d,%d}.AtLeast(3.2) = %v, want %v", c.v.Major, c.v.Minor, got, c.ok)
		}
	}
}

func TestVersionString(t *testing.T) {
	if got := (Version{3, 2}).String(); got != "3.2" {
		t.Errorf("String() = %q, want %q", got, "3.2")
	}
}

// `tmux -C ls` with no server running proves control mode works; there is simply
// nothing to list. Any other failure means control mode itself is unusable.
func TestClassifyControlMode(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		stderr string
		ok     bool
	}{
		{name: "server running", err: nil, ok: true},
		{name: "no server yet", err: errExit, stderr: "no server running on /tmp/tmux-1000/default", ok: true},
		{name: "no current server", err: errExit, stderr: "error connecting to /tmp/tmux-1000/default (No such file or directory)", ok: true},
		{name: "unknown flag", err: errExit, stderr: "unknown option -- C", ok: false},
		{name: "socket dir uncreatable", err: errExit, stderr: "couldn't create directory /nope/tmux-1000 (No such file or directory)", ok: false},
		{name: "socket dir unreadable", err: errExit, stderr: "couldn't create directory /locked/tmux-1000 (Permission denied)", ok: false},
		{name: "usage error", err: errExit, stderr: "usage: tmux [-2CDlNuVv]", ok: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, detail := classifyControlMode(c.err, c.stderr)
			if ok != c.ok {
				t.Errorf("classifyControlMode(%v, %q) = %v, want %v", c.err, c.stderr, ok, c.ok)
			}
			if !ok && detail == "" {
				t.Error("a failure must explain itself")
			}
		})
	}
}

func TestCheckWritable(t *testing.T) {
	dir := t.TempDir()
	if err := checkWritable(filepath.Join(dir, "state", "workspaces")); err != nil {
		t.Errorf("a creatable directory should be writable: %v", err)
	}

	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission checks are meaningless")
	}
	if err := checkWritable(filepath.Join(locked, "state")); err == nil {
		t.Error("a directory that cannot be created should be reported as not writable")
	}
}

func TestReportFailsWhenAnyCheckFails(t *testing.T) {
	if !OK([]Result{{Name: "a", OK: true}, {Name: "b", OK: true}}) {
		t.Error("all-passing results should report OK")
	}
	if OK([]Result{{Name: "a", OK: true}, {Name: "b", OK: false, Detail: "nope"}}) {
		t.Error("one failing check should fail the report")
	}
}

func TestFormatNamesTheFailingCheck(t *testing.T) {
	out := Format([]Result{{Name: "tmux", OK: false, Detail: "tmux 3.1c is older than the required 3.2"}})
	if !strings.Contains(out, "tmux") || !strings.Contains(out, "3.2") {
		t.Errorf("a failure report must name the check and the problem, got:\n%s", out)
	}
}
