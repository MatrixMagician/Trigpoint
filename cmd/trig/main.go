// Command trig is Trigpoint: a keyboard-driven spatial map over long-lived
// terminal sessions.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/doctor"
	"github.com/MatrixMagician/Trigpoint/internal/hooks"
	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/status"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
	"github.com/MatrixMagician/Trigpoint/internal/tui"
)

// errReported stands for a failure whose message has already reached the user —
// flag's own parse errors, which it prints to stderr before returning them.
var errReported = errors.New("already reported")

func main() {
	if err := run(os.Args[1:]); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintln(os.Stderr, "trig:", err)
		}
		os.Exit(1)
	}
}

// parse runs fs over args, distinguishing a request for help — which is not a
// failure — from a genuine parse error, which flag has already reported.
func parse(fs *flag.FlagSet, args []string) (helped bool, err error) {
	switch err := fs.Parse(args); {
	case err == nil:
		return false, nil
	case errors.Is(err, flag.ErrHelp):
		return true, nil
	default:
		return false, errReported
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "new":
			return runNew(args[1:])
		case "ls":
			return runLs(args[1:])
		case "attach":
			return runAttach(args[1:])
		case "doctor":
			return runDoctor(args[1:])
		case "emit-status":
			return runEmitStatus(args[1:])
		case "init-hooks":
			return runInitHooks(args[1:])
		}
	}
	return runTUI(args)
}

func runTUI(args []string) error {
	fs := flag.NewFlagSet("trig", flag.ContinueOnError)
	workspace := fs.String("w", "", "workspace to open (default: the configured default workspace)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `usage: trig [-w workspace]
       trig new [-w workspace] [-k kind] [-t title] [--cmd command] [-d dir]
       trig ls [-w workspace] [--json]
       trig attach [-w workspace] <node>
       trig doctor
       trig emit-status <state> [detail...]
       trig init-hooks claude

flags:
`)
		fs.PrintDefaults()
	}
	if helped, err := parse(fs, args); helped || err != nil {
		return err
	}

	if fs.NArg() > 0 {
		// Most often a subcommand that was mistyped: without this, `trig sl`
		// opens the map rather than saying there is no such command.
		fs.Usage()
		return fmt.Errorf("unexpected argument %q: trig opens the map, and a workspace is named with -w", fs.Arg(0))
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	name := cfg.General.DefaultWorkspace
	if *workspace != "" {
		name = *workspace
	}
	if err := state.ValidName(name); err != nil {
		return err
	}

	stateDir, err := state.Dir()
	if err != nil {
		return err
	}
	ws, err := state.Load(stateDir, name)
	if err != nil {
		return err
	}

	// The keymap is resolved before the terminal is taken, so a config that
	// binds a key twice is a message on stderr rather than a key that quietly
	// does nothing (§10).
	model, err := tui.New(cfg, ws, stateDir, tmuxCLI())
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
	return err
}

// tmuxCLI is the one place a tmux client is built, so that every command talks
// to the same server. The socket is read from the environment for the tests,
// which run against a private server so that nothing they do can touch the
// sessions the user is actually working in; unset — the ordinary case — is
// tmux's own default server.
func tmuxCLI() tmux.CLI { return tmux.CLI{Socket: os.Getenv("TRIG_TMUX_SOCKET")} }

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("trig doctor", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: trig doctor\n\nChecks whether this machine can run Trigpoint.\n")
	}
	if helped, err := parse(fs, args); helped || err != nil {
		return err
	}

	cfgPath, err := config.Path()
	if err != nil {
		return err
	}
	stateDir, err := state.Dir()
	if err != nil {
		return err
	}
	claudeSettings, err := hooks.SettingsPath()
	if err != nil {
		return err
	}

	results := append(doctor.Run(cfgPath, stateDir, claudeSettings), checkKeymap(cfgPath))
	fmt.Print(doctor.Format(results))
	if !doctor.OK(results) {
		return fmt.Errorf("doctor found problems; Trigpoint will not run correctly until they are fixed")
	}
	return nil
}

// checkKeymap asks the question the map asks at startup: does [keymap] resolve?
// A keymap that does not is a map that will not open, so doctor says which
// binding is wrong rather than passing a config Trigpoint is about to refuse.
//
// It lives here rather than in internal/doctor because the actions it validates
// against are the map view's own, and a diagnostic that draws nothing should not
// be the reason internal/doctor depends on a package full of rendering.
func checkKeymap(path string) doctor.Result {
	const name = "keymap"
	cfg, err := config.Load(path)
	if err != nil {
		// The config check has already said so, in its own words; repeating it
		// under a second heading would read as two problems.
		return doctor.Result{Name: name, OK: true, Detail: "not checked: the config did not load"}
	}
	if _, err := tui.NewKeymap(cfg.Keymap); err != nil {
		return doctor.Result{Name: name, Detail: err.Error()}
	}
	return doctor.Result{Name: name, OK: true, Detail: "every binding resolves"}
}

// runEmitStatus is the agent side of the status contract (SPEC §8): an agent —
// or its hooks, or the user by hand from inside a node — says what it is doing,
// and Trigpoint reads it off disk. The path comes from the environment rather
// than the command line, because it is a session Trigpoint set up and the agent
// is not expected to know where Trigpoint keeps anything.
//
// It is a convenience over the file format and never a privileged path: an
// agent that writes the JSON itself is exactly as integrated as one that shells
// out to this.
func runEmitStatus(args []string) error {
	fs := flag.NewFlagSet("trig emit-status", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `usage: trig emit-status <state> [detail...]

Writes an agent status report to the file named by $TRIG_STATUS_FILE, which
Trigpoint puts into every agent node's session environment.

States: %s

The words after the state are the detail, shown beside the badge on the map.
`, statesList())
	}
	if helped, err := parse(fs, args); helped || err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return fmt.Errorf("no state given: an agent reports one of %s", statesList())
	}

	// Checked before the file is looked for, so that a typo is answered with
	// the vocabulary rather than with a complaint about the environment.
	state, err := status.ParseState(fs.Arg(0))
	if err != nil {
		return err
	}
	path := os.Getenv("TRIG_STATUS_FILE")
	if path == "" {
		return errors.New("TRIG_STATUS_FILE is not set: agent status is reported from inside an agent node's session, where Trigpoint puts it")
	}
	return status.Write(path, state, strings.Join(fs.Args()[1:], " "))
}

// runInitHooks installs the plumbing that makes a Claude Code node report its
// own status (SPEC §8). It is a command the user runs, never something node
// creation does on their behalf: this writes to a file Trigpoint does not own,
// and a tool that edits your agent's configuration without being asked is one
// you stop trusting with it.
func runInitHooks(args []string) error {
	fs := flag.NewFlagSet("trig init-hooks", flag.ContinueOnError)
	dryRun := fs.Bool("n", false, "print what would change and write nothing")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `usage: trig init-hooks claude [-n]

Merges the hook entries that make a Claude Code node report its status into
your Claude Code settings, leaving everything else in the file alone. Running
it again changes nothing.

flags:
`)
		fs.PrintDefaults()
	}
	// flag stops parsing at the first non-flag argument, so the agent name comes
	// off the front before the flags are read. Without this, `init-hooks claude
	// -n` — the form the usage line prints — takes -n as a second positional
	// and is refused.
	agent := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agent, args = args[0], args[1:]
	}
	if helped, err := parse(fs, args); helped || err != nil {
		return err
	}
	extra := fs.Args()
	if agent == "" && len(extra) > 0 {
		agent, extra = extra[0], extra[1:]
	}
	// One known agent so far. Every other agent integrates through the file
	// format instead, which is the documented contract and needs no command.
	if agent != "claude" || len(extra) > 0 {
		fs.Usage()
		return errors.New("say which agent's hooks to install; the one Trigpoint knows how to configure is claude")
	}

	path, err := hooks.SettingsPath()
	if err != nil {
		return err
	}
	added, err := hooks.Install(path, *dryRun)
	if err != nil {
		return err
	}
	if len(added) == 0 {
		fmt.Printf("%s: the hooks are already installed; nothing to do\n", path)
		return nil
	}

	verb := "added to"
	if *dryRun {
		verb = "would be added to"
	}
	fmt.Printf("%d %s %s:\n", len(added), verb, path)
	for _, e := range added {
		fmt.Printf("  %-17s %s\n      %s (%s)\n", e.Event, e.Command, e.State, e.Why)
	}
	if *dryRun {
		fmt.Println("nothing was written; run without -n to install")
	}
	return nil
}

func statesList() string {
	names := make([]string, len(status.States))
	for i, s := range status.States {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
}

func loadConfig() (config.Config, error) {
	path, err := config.Path()
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(path)
}
