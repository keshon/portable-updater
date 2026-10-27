package updater

import (
	"errors"
	"fmt"

	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/platform"
)

func (u *transaction) rollback() error {
	if u.state.Phase != "applied" {
		return errors.New("no applied update to roll back")
	}
	runtime := u.runtime()
	previous, e := files.Join(u.root, u.state.Previous)
	if e != nil {
		return e
	}
	if !files.Exists(previous) {
		return errors.New("previous runtime missing")
	}
	if e = platform.GuardProcesses(runtime, previous); e != nil {
		return e
	}
	rejected := u.runtimeCopy("rejected", files.Stamp())
	dst, e := files.Join(u.root, rejected)
	if e != nil {
		return e
	}
	// Stage records the displaced current runtime for recovery after interruption.
	u.state.Stage = rejected
	u.state.Phase = "rolling_back"
	if e = u.save(); e != nil {
		return e
	}
	if e = renamePath(runtime, dst); e != nil {
		u.state.Phase = "applied"
		_ = u.save()
		return e
	}
	if e = renamePath(previous, runtime); e != nil {
		restore := renamePath(dst, runtime)
		if restore == nil {
			u.state.Phase = "applied"
			_ = u.save()
		}
		return fmt.Errorf("rollback failed: %v; restoration: %v", e, restore)
	}
	u.state.Phase = "rolled_back"
	if e = u.save(); e != nil {
		return e
	}
	fmt.Fprintln(u.output(), "Previous runtime restored. External saves kept at their current state. Rejected runtime:", dst)
	return nil
}
