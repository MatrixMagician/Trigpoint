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
		case "doctor":
			return runDoctor(args[1:])
		case "emit-status":
			return runEmitStatus(args[1:])
		}
	}
	return runTUI(args)
}

func runTUI(args []string) error {
	fs := flag.NewFlagSet("trig", flag.ContinueOnError)
	workspace := fs.String("w", "", "workspace to open (default: the configured default workspace)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: trig [-w workspace]\n       trig doctor\n       trig emit-status <state> [detail...]\n\nflags:\n")
		fs.PrintDefaults()
	}
	if helped, err := parse(fs, args); helped || err != nil {
		return err
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

	_, err = tea.NewProgram(tui.New(cfg, ws, stateDir, tmux.CLI{}), tea.WithAltScreen()).Run()
	return err
}

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

	results := doctor.Run(cfgPath, stateDir)
	fmt.Print(doctor.Format(results))
	if !doctor.OK(results) {
		return fmt.Errorf("doctor found problems; Trigpoint will not run correctly until they are fixed")
	}
	return nil
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
