package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/keshon/portable-updater/internal/config"
	"github.com/keshon/portable-updater/internal/platform"
	"github.com/keshon/portable-updater/internal/updater"
)

func Run(args []string, stdout, stderr io.Writer) error {
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	flags := flag.NewFlagSet("portable-update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("manifest", filepath.Join(filepath.Dir(executable), "update.json"), "container configuration")
	rollback := flags.Bool("rollback", false, "restore previous emulator")
	if e = flags.Parse(args); e != nil {
		return e
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("no command is required: run the program directly")
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
	display := io.MultiWriter(stdout, log)
	if *rollback {
		e = project.Restore(display)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		e = project.Update(ctx, display)
	}
	if e != nil {
		fmt.Fprintln(log, "ERROR:", e)
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
