//go:build !windows

package console

import "io"

func terminalColor(io.Writer) bool { return false }
