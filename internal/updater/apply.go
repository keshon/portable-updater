package updater

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/platform"
)

func (u *transaction) apply(skipSnapshot bool) error {
	if u.state.Phase != "staged" {
		return errors.New("stage a candidate first")
	}
	runtime := u.runtime()
	stage, e := files.Join(u.root, u.state.Stage)
	if e != nil {
		return e
	}
	if e = files.ValidateTree(runtime); e != nil {
		return e
	}
	if e = files.ValidateTree(stage); e != nil {
		return e
	}
	if !files.Exists(filepath.Join(stage, u.policy.Executable)) {
		return errors.New("candidate executable missing")
	}
	if e = platform.GuardProcesses(runtime, stage); e != nil {
		return e
	}
	id := files.Stamp()
	if !skipSnapshot {
		var total int64
		for _, r := range u.policy.Saves {
			p, e := files.Join(u.root, r)
			if e != nil {
				return e
			}
			if !files.Exists(p) {
				continue
			}
			n, e := files.Size(p)
			if e != nil {
				return e
			}
			total += n
		}
		if total > 2<<30 {
			return errors.New("save snapshot exceeds 2 GiB; current emulator kept. Review snapshot paths in util/portable-update/update.json")
		}
		u.state.Snapshot = "tmp/portable-update/snapshots/" + id
		for _, r := range u.policy.Saves {
			p, e := files.Join(u.root, r)
			if e != nil {
				return e
			}
			if files.Exists(p) {
				dst, e := files.Join(u.root, u.state.Snapshot+"/"+filepath.ToSlash(r))
				if e != nil {
					return e
				}
				if e = files.CopyTree(p, dst); e != nil {
					return e
				}
			}
		}
	}
	// Move candidate defaults aside without deleting anything; then copy LIVE settings.
	for _, r := range u.policy.Preserve {
		src, e := files.Join(runtime, r)
		if e != nil {
			return e
		}
		dst, e := files.Join(stage, r)
		if e != nil {
			return e
		}
		if files.Exists(dst) {
			arch, e := files.Join(u.work, "package-defaults/"+id+"/"+filepath.ToSlash(r))
			if e != nil {
				return e
			}
			if e = os.MkdirAll(filepath.Dir(arch), 0755); e != nil {
				return e
			}
			if e = renamePath(dst, arch); e != nil {
				return e
			}
		}
		if files.Exists(src) {
			if e = files.CopyTree(src, dst); e != nil {
				return e
			}
		}
	}
	if e = platform.GuardProcesses(runtime, stage); e != nil {
		return e
	}
	u.state.Previous = u.runtimeCopy("previous", id)
	previous, e := files.Join(u.root, u.state.Previous)
	if e != nil {
		return e
	}
	u.state.Phase = "swapping"
	if e = u.save(); e != nil {
		return e
	}
	if e = renamePath(runtime, previous); e != nil {
		u.state.Phase = "staged"
		_ = u.save()
		return e
	}
	u.state.Phase = "old_moved"
	if e = u.save(); e != nil {
		return fmt.Errorf("old runtime moved; rerun the updater to recover: %w", e)
	}
	if e = renamePath(stage, runtime); e != nil {
		restore := renamePath(previous, runtime)
		if restore == nil {
			u.state.Phase = "staged"
			_ = u.save()
		}
		return fmt.Errorf("candidate swap failed: %v; restoration: %v", e, restore)
	}
	u.state.Phase = "applied"
	if e = u.save(); e != nil {
		return fmt.Errorf("runtime replaced; recover journal before another update: %w", e)
	}
	fmt.Fprintln(u.output(), "Applied. Previous runtime retained:", previous)
	fmt.Fprintln(u.output(), "External saves were not replaced. Complete a save/load check; use rollback if needed.")
	return nil
}
