package updater

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/platform"
)

type journal struct {
	Phase    string `json:"phase"`
	Stage    string `json:"stage"`
	Previous string `json:"previous,omitempty"`
	Snapshot string `json:"snapshot,omitempty"`
	Source   string `json:"source"`
	Created  string `json:"created"`
}

func (u *transaction) journalPath() string {
	return filepath.Join(u.home, "state", "update-state.json")
}

func jsonFile(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	tmp := path + ".writing-" + files.Stamp()
	if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	if e = os.WriteFile(tmp, append(b, '\n'), 0644); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}

func (u *transaction) save() error { return jsonFile(u.journalPath(), u.state) }

func (u *transaction) recover() error {
	runtime := u.runtime()
	stage, e := files.Join(u.root, u.state.Stage)
	if e != nil {
		return e
	}
	previous, e := files.Join(u.root, u.state.Previous)
	if e != nil {
		return e
	}
	if e = platform.GuardProcesses(runtime, stage, previous); e != nil {
		return e
	}
	switch u.state.Phase {
	case "swapping", "old_moved":
		if !files.Exists(runtime) && files.Exists(previous) {
			if e = renamePath(previous, runtime); e != nil {
				return e
			}
			u.state.Phase = "staged"
		} else if files.Exists(runtime) && !files.Exists(stage) && files.Exists(previous) {
			u.state.Phase = "applied"
		} else if files.Exists(runtime) && files.Exists(stage) && !files.Exists(previous) {
			u.state.Phase = "staged"
		} else {
			return errors.New("ambiguous recovery layout; inspect journal and retained folders")
		}
	case "rolling_back":
		if !files.Exists(runtime) && files.Exists(stage) && files.Exists(previous) {
			if e = renamePath(stage, runtime); e != nil {
				return e
			}
			u.state.Phase = "applied"
		} else if files.Exists(runtime) && files.Exists(stage) && !files.Exists(previous) {
			u.state.Phase = "rolled_back"
		} else if files.Exists(runtime) && !files.Exists(stage) && files.Exists(previous) {
			u.state.Phase = "applied"
		} else {
			return errors.New("ambiguous rollback layout")
		}
	default:
		return errors.New("no interrupted swap to recover")
	}
	return u.save()
}
