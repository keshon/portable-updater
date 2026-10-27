package main

import (
	"fmt"
	"os"

	"github.com/keshon/portable-updater/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "Could not complete the update:", err)
		fmt.Fprintln(os.Stderr, "Details: tmp/portable-update/update.log")
		os.Exit(1)
	}
}
