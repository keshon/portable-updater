package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/keshon/portable-updater/internal/config"
	"github.com/keshon/portable-updater/internal/console"
	"github.com/keshon/portable-updater/internal/platform"
	"github.com/keshon/portable-updater/internal/selfupdate"
	"github.com/keshon/portable-updater/internal/setup"
	"github.com/keshon/portable-updater/internal/updater"
)

var Version = "dev"

func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 2 && args[0] == "--internal-self-update" {
		return selfupdate.Apply(args[1], stdout)
	}
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	return run(args, stdout, stderr, executable, os.Stdin)
}

func run(args []string, stdout, stderr io.Writer, executable string, input io.Reader) error {
	flags := flag.NewFlagSet("portable-update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("manifest", filepath.Join(filepath.Dir(executable), "update.json"), "container configuration")
	rollback := flags.Bool("rollback", false, "restore previous runtime")
	setupRequested := flags.Bool("setup", false, "configure an existing portable application")
	versionRequested := flags.Bool("version", false, "print updater version")
	selfRequested := flags.Bool("self-update", false, "update this installed copy of Portable Updater")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("no command is required: run the program directly")
	}
	if *selfRequested {
		conflict := false
		flags.Visit(func(f *flag.Flag) {
			if f.Name != "self-update" {
				conflict = true
			}
		})
		if conflict {
			return fmt.Errorf("--self-update must be used on its own")
		}
	}
	if *versionRequested {
		fmt.Fprintln(stdout, "Portable Updater", Version)
		return nil
	}
	if *selfRequested {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		return selfupdate.Start(ctx, executable, Version, stdout)
	}
	manifestProvided := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "manifest" {
			manifestProvided = true
		}
	})
	_, manifestError := os.Stat(*manifest)
	if *setupRequested || (!*rollback && !manifestProvided && errors.Is(manifestError, os.ErrNotExist)) {
		return setup.Wizard(input, stdout, executable)
	}
	configuration, e := config.Load(*manifest)
	if e != nil {
		return e
	}
	project, e := updater.New(configuration)
	if e != nil {
		return e
	}
	// Windows locks are released by the OS even if this process is interrupted.
	unlock, e := platform.LockUpdater(filepath.Join(configuration.Home, "updater.lock"))
	if e != nil {
		return e
	}
	defer unlock()
	log, e := openLog(configuration.Work)
	if e != nil {
		return e
	}
	defer log.Close()
	fmt.Fprintln(log, "\nRun:", time.Now().UTC().Format(time.RFC3339))
	project.SetOutput(log)
	display := console.WithLog(stdout, log)
	fmt.Fprintln(display, "Portable Updater", Version)
	if *rollback {
		e = project.Restore(display)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		e = project.Update(ctx, display)
	}
	if e != nil {
		fmt.Fprintln(log, "ERROR:", e)
		return fmt.Errorf("%w (log: %s)", e, filepath.Join(configuration.Work, "update.log"))
	}
	return e
}
func openLog(util string) (*os.File, error) {
	dir := util
	if e := os.MkdirAll(dir, 0755); e != nil {
		return nil, e
	}
	return os.OpenFile(filepath.Join(dir, "update.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
}
