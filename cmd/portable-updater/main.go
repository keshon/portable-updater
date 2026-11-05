package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/keshon/portable-updater/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "Could not complete the operation:", err)
		fmt.Fprintln(os.Stderr, "For update failures, see tmp/portable-update/update.log in the container.")
		os.Exit(1)
	}
}
