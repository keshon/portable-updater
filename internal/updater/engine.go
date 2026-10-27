package updater

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/portable-updater/internal/config"
	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/release"
)

type Engine struct {
	*transaction
	Instance config.Instance
	Adapter  config.Adapter
	provider release.ReleaseProvider
}

func New(c *config.Config) (*Engine, error) {
	root, home, instance, adapter := c.Root, c.Home, c.Instance, c.Adapter
	var e error
	u := &transaction{root: root, home: home, work: c.Work, policy: installPolicy{Name: instance.Name, Runtime: instance.Runtime, Executable: adapter.Executable, Preserve: adapter.Preserve, Saves: instance.Saves}}
	if b, e := os.ReadFile(u.journalPath()); e == nil {
		if e = json.Unmarshal(b, &u.state); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	for _, p := range []string{u.state.Stage, u.state.Previous, u.state.Snapshot} {
		if p != "" {
			if _, e = files.Join(root, p); e != nil {
				return nil, e
			}
		}
	}
	if u.state.Previous != "" && !strings.HasPrefix(filepath.ToSlash(u.state.Previous), u.runtimeCopy("previous", "")) {
		return nil, errors.New("journal previous runtime does not match this container")
	}
	if u.state.Stage != "" && !strings.HasPrefix(filepath.ToSlash(u.state.Stage), u.runtimeCopy("next", "")) && !strings.HasPrefix(filepath.ToSlash(u.state.Stage), u.runtimeCopy("rejected", "")) {
		return nil, errors.New("journal candidate does not match this runtime")
	}

	return &Engine{transaction: u, Instance: instance, Adapter: adapter}, nil
}
func (e *Engine) SetOutput(out io.Writer) { e.logWriter = out }
