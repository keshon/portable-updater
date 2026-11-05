package console

import (
	"bytes"
	"strings"
	"testing"
)

func TestStylesStayOutOfLogs(t *testing.T) {
	var out, log bytes.Buffer
	w := &Writer{out: &out, log: &log, color: true}
	w.Heading("Setup %s", "Example")
	w.Success("Ready")
	w.Write([]byte("Path: C:\\Games\\Example\n"))
	if !strings.Contains(out.String(), "\x1b[36m") || !strings.Contains(out.String(), "\x1b[32m") {
		t.Fatal("missing terminal accents")
	}
	if strings.Contains(log.String(), "\x1b[") || log.String() != "Setup Example\nReady\nPath: C:\\Games\\Example\n" {
		t.Fatal(log.String())
	}
}

func TestRedirectedOutputIsPlain(t *testing.T) {
	var out bytes.Buffer
	New(&out).Error("Could not continue")
	if out.String() != "Could not continue\n" {
		t.Fatal(out.String())
	}
}
