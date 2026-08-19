// Command trig is Trigpoint: a keyboard-driven spatial map over long-lived
// terminal sessions.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/doctor"
	"github.com/MatrixMagician/Trigpoint/internal/state"
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
	if len(args) > 0 && args[0] == "doctor" {
		return runDoctor(args[1:])
	}
	return runTUI(args)
}

func runTUI(args []string) error {
	fs := flag.NewFlagSet("trig", flag.ContinueOnError)
	workspace := fs.String("w", "", "workspace to open (default: the configured default workspace)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: trig [-w workspace]\n       trig doctor\n\nflags:\n")
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

	_, err = tea.NewProgram(tui.New(cfg, ws), tea.WithAltScreen()).Run()
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

func loadConfig() (config.Config, error) {
	path, err := config.Path()
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(path)
}
