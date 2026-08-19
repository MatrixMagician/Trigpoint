// Package doctor answers one question: can this machine run Trigpoint? Every
// check either passes or explains, in its own words, what is wrong — a failure
// that only says "failed" sends the user hunting.
package doctor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/MatrixMagician/Trigpoint/internal/config"
)

// minTmux is the oldest tmux Trigpoint supports: control mode, `capture-pane -e`,
// session environment, and the `refresh-client -B` subscription the preview
// monitor's activity events arrive on all have to be there.
var minTmux = Version{Major: 3, Minor: 2}

type Version struct{ Major, Minor int }

func (v Version) String() string { return fmt.Sprintf("%d.%d", v.Major, v.Minor) }

func (v Version) AtLeast(min Version) bool {
	if v.Major != min.Major {
		return v.Major > min.Major
	}
	return v.Minor >= min.Minor
}

// Result is one check and what it found.
type Result struct {
	Name   string
	OK     bool
	Detail string
}

// Run performs every check and returns the results in a fixed order.
func Run(cfgPath, stateDir string) []Result {
	tmuxOK, tmuxResult := checkTmux()
	return []Result{
		tmuxResult,
		checkControlMode(tmuxOK),
		checkConfig(cfgPath),
		checkStateDir(stateDir),
	}
}

// OK reports whether every check passed.
func OK(results []Result) bool {
	for _, r := range results {
		if !r.OK {
			return false
		}
	}
	return true
}

// Format renders results for a terminal, naming the problem on every failure.
func Format(results []Result) string {
	var b strings.Builder
	for _, r := range results {
		if r.OK {
			fmt.Fprintf(&b, "  ok    %s", r.Name)
			if r.Detail != "" {
				fmt.Fprintf(&b, " (%s)", r.Detail)
			}
			b.WriteByte('\n')
			continue
		}
		fmt.Fprintf(&b, "  FAIL  %s: %s\n", r.Name, r.Detail)
	}
	return b.String()
}

func checkTmux() (bool, Result) {
	const name = "tmux"
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, Result{Name: name, Detail: "tmux is not installed or not on PATH; Trigpoint needs tmux " + minTmux.String() + " or newer"}
		}
		return false, Result{Name: name, Detail: fmt.Sprintf("could not run `tmux -V`: %v", err)}
	}

	v, err := parseTmuxVersion(string(out))
	if err != nil {
		return false, Result{Name: name, Detail: fmt.Sprintf("could not read a version from `tmux -V` output %q", strings.TrimSpace(string(out)))}
	}
	if !v.AtLeast(minTmux) {
		return false, Result{Name: name, Detail: fmt.Sprintf("tmux %s is older than the required %s", v, minTmux)}
	}
	return true, Result{Name: name, OK: true, Detail: v.String()}
}

func checkControlMode(tmuxOK bool) Result {
	const name = "control mode"
	if !tmuxOK {
		return Result{Name: name, Detail: "not checked: tmux is unusable (see above)"}
	}
	// `tmux -C ls` exercises control mode itself. With no server running there is
	// simply nothing to list, which still proves the flag works.
	cmd := exec.Command("tmux", "-C", "ls")
	_, err := cmd.Output()
	stderr := ""
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr = string(exitErr.Stderr)
	}
	if ok, detail := classifyControlMode(err, stderr); !ok {
		return Result{Name: name, Detail: detail}
	}
	return Result{Name: name, OK: true}
}

// classifyControlMode decides whether a `tmux -C ls` failure was control mode not
// working or merely no server being up yet.
func classifyControlMode(err error, stderr string) (bool, string) {
	if err == nil {
		return true, ""
	}
	// Only "there is no server yet" is benign. A missing or unusable socket
	// directory reports its own "(No such file or directory)", and matching that
	// text alone would wave through exactly the breakage this check exists for.
	s := strings.ToLower(stderr)
	for _, benign := range []string{"no server running", "error connecting to"} {
		if strings.Contains(s, benign) {
			return true, ""
		}
	}
	detail := strings.TrimSpace(stderr)
	if detail == "" {
		detail = err.Error()
	}
	return false, fmt.Sprintf("`tmux -C ls` failed: %s", detail)
}

func checkConfig(path string) Result {
	const name = "config"
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return Result{Name: name, OK: true, Detail: "no file at " + path + "; using defaults"}
	}
	if _, err := config.Load(path); err != nil {
		return Result{Name: name, Detail: err.Error()}
	}
	return Result{Name: name, OK: true, Detail: path}
}

func checkStateDir(dir string) Result {
	const name = "state directory"
	if err := checkWritable(dir); err != nil {
		return Result{Name: name, Detail: err.Error()}
	}
	return Result{Name: name, OK: true, Detail: dir}
}

// checkWritable proves the directory can actually be written to, rather than
// trusting its mode bits.
func checkWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	probe, err := os.CreateTemp(dir, ".trig-doctor-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	name := probe.Name()
	probe.Close()
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("cannot remove files in %s: %w", filepath.Dir(name), err)
	}
	return nil
}

var tmuxVersionRe = regexp.MustCompile(`(\d+)\.(\d+)`)

// parseTmuxVersion pulls a version out of `tmux -V` output, which is variously
// "tmux 3.2a", "tmux 3.7b", or "tmux next-3.4".
func parseTmuxVersion(out string) (Version, error) {
	m := tmuxVersionRe.FindStringSubmatch(out)
	if m == nil {
		return Version{}, fmt.Errorf("no version in %q", strings.TrimSpace(out))
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return Version{}, err
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		return Version{}, err
	}
	return Version{Major: major, Minor: minor}, nil
}
