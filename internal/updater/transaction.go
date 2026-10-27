package updater

import (
	"io"
	"os"
	"path/filepath"
)

type installPolicy struct {
	Name       string
	Runtime    string
	Executable string
	Preserve   []string
	Saves      []string
}

type transaction struct {
	root, home, work string
	policy           installPolicy
	state            journal
	logWriter        io.Writer
}

var renamePath = os.Rename

func (u *transaction) runtime() string {
	return filepath.Join(u.root, filepath.FromSlash(u.policy.Runtime))
}

func (u *transaction) output() io.Writer {
	if u.logWriter != nil {
		return u.logWriter
	}
	return os.Stdout
}

// Retained candidates and previous runtimes belong to the container's temporary workspace.
func (u *transaction) runtimeCopy(kind, id string) string {
	return filepath.ToSlash(filepath.Join("tmp", "portable-update", filepath.Base(filepath.FromSlash(u.policy.Runtime))+"."+kind+"-"+id))
}
