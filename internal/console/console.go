// Package console provides optional terminal accents without styling logs.
package console

import (
	"fmt"
	"io"
	"os"
)

type Writer struct {
	out   io.Writer
	log   io.Writer
	color bool
}

func New(out io.Writer) *Writer {
	if existing, ok := out.(*Writer); ok {
		return existing
	}
	return &Writer{out: out, color: os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" && terminalColor(out)}
}

func WithLog(out, log io.Writer) *Writer {
	w := New(out)
	return &Writer{out: w.out, log: log, color: w.color}
}

func (w *Writer) Write(p []byte) (int, error) {
	if w.log != nil {
		return io.MultiWriter(w.out, w.log).Write(p)
	}
	return w.out.Write(p)
}

func (w *Writer) line(color, format string, args ...any) {
	plain := fmt.Sprintf(format, args...) + "\n"
	if w.color {
		fmt.Fprint(w.out, "\x1b["+color+"m"+plain+"\x1b[0m")
	} else {
		fmt.Fprint(w.out, plain)
	}
	if w.log != nil {
		fmt.Fprint(w.log, plain)
	}
}

func (w *Writer) Heading(format string, args ...any) { w.line("36", format, args...) }
func (w *Writer) Action(format string, args ...any)  { w.line("36", format, args...) }
func (w *Writer) Success(format string, args ...any) { w.line("32", format, args...) }
func (w *Writer) Warning(format string, args ...any) { w.line("33", format, args...) }
func (w *Writer) Error(format string, args ...any)   { w.line("31", format, args...) }
