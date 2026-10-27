package updater

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/platform"
)

func (u *transaction) stage(source string) error {
	if u.state.Phase == "staged" || u.state.Phase == "swapping" || u.state.Phase == "old_moved" || u.state.Phase == "rolling_back" {
		return errors.New("pending transaction; apply or recover it first")
	}
	src, e := filepath.Abs(source)
	if e != nil {
		return e
	}
	src, e = filepath.EvalSymlinks(src)
	if e != nil {
		return e
	}
	runtime := u.runtime()
	if files.Within(runtime, src) || files.Within(src, runtime) {
		return errors.New("source overlaps installed runtime")
	}
	if e = files.ValidateTree(runtime); e != nil {
		return e
	}
	if e = platform.GuardProcesses(runtime); e != nil {
		return e
	}
	id := files.Stamp()
	rel := u.runtimeCopy("next", id)
	dest, e := files.Join(u.root, rel)
	if e != nil {
		return e
	}
	if files.Within(src, dest) {
		return errors.New("stage is inside source")
	}
	if e = os.MkdirAll(u.work, 0755); e != nil {
		return e
	}
	if e = os.Mkdir(dest, 0755); e != nil {
		return e
	}
	// Failed extraction is retained, never mistaken for an approved stage.
	unpack := filepath.Join(u.work, "unpacked-"+id)
	info, e := os.Stat(src)
	if e != nil {
		return e
	}
	if info.IsDir() {
		if e = files.ValidateTree(src); e != nil {
			return e
		}
		if e = files.CopyTree(src, dest); e != nil {
			return e
		}
	} else {
		if e = os.MkdirAll(u.work, 0755); e != nil {
			return e
		}
		if e = os.Mkdir(unpack, 0755); e != nil {
			return e
		}
		if e = files.Extract(src, unpack); e != nil {
			return e
		}
		var matches []string
		e = filepath.WalkDir(unpack, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.IsDir() && strings.EqualFold(d.Name(), filepath.Base(u.policy.Executable)) {
				matches = append(matches, filepath.Dir(p))
			}
			return nil
		})
		if e != nil {
			return e
		}
		if len(matches) != 1 {
			return fmt.Errorf("package must contain one %s (found %d)", u.policy.Executable, len(matches))
		}
		if e = files.CopyTree(matches[0], dest); e != nil {
			return e
		}
	}
	exe, e := files.Join(dest, u.policy.Executable)
	if e != nil {
		return e
	}
	if !files.Exists(exe) {
		return errors.New("candidate entry point missing; select the extracted runtime directory")
	}
	if u.state.Phase != "" {
		if e = jsonFile(filepath.Join(u.work, "history", id+".json"), u.state); e != nil {
			return e
		}
	}
	u.state = journal{Phase: "staged", Stage: rel, Source: src, Created: id}
	if e = u.save(); e != nil {
		return e
	}
	fmt.Fprintln(u.output(), "Staged clean package:", dest)
	fmt.Fprintln(u.output(), "Settings will be copied from the installed runtime immediately before apply. Do not launch this clean stage.")
	return nil
}
