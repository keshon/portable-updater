package main

import (
	"errors"
	"flag"
	"os"

	"github.com/keshon/portable-updater/internal/app"
	"github.com/keshon/portable-updater/internal/console"
)

func main() {
	if err := app.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		console.New(os.Stderr).Error("Could not complete the operation: %s", err)
		os.Exit(1)
	}
}
