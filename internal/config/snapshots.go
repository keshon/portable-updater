package config

import (
	"errors"
	"fmt"

	"github.com/keshon/portable-updater/internal/files"
)

// ValidateSnapshotPaths is shared by loading and first-time setup. Missing
// selections are allowed here; the interactive Add action requires existence.
func ValidateSnapshotPaths(root string, paths []string) error {
	var targets []string
	for _, path := range paths {
		target, err := files.Join(root, path)
		if err != nil {
			return err
		}
		target, err = files.Canonical(target)
		if err != nil {
			return err
		}
		for _, reserved := range []string{"util/portable-update", "tmp/portable-update"} {
			boundary, e := files.Join(root, reserved)
			if e != nil {
				return e
			}
			boundary, e = files.Canonical(boundary)
			if e != nil {
				return e
			}
			if files.Within(boundary, target) || files.Within(target, boundary) {
				return errors.New("backup path overlaps updater storage")
			}
		}
		for i, other := range targets {
			if files.Within(other, target) || files.Within(target, other) {
				return fmt.Errorf("backup selection overlaps %s; select each file or folder only once", paths[i])
			}
		}
		targets = append(targets, target)
	}
	return nil
}
