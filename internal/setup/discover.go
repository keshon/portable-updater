package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/portable-updater/internal/files"
)

type Candidate struct {
	Root    string
	Runtime string
	Profile Profile
}

func (p Profile) Name() string {
	if p.Adapter.DisplayName != "" {
		return p.Adapter.DisplayName
	}
	return p.ID
}

// Discover checks the selection itself, default layouts and immediate child
// folders. It never searches a drive recursively or chooses between matches.
func Discover(selection string, profiles []Profile) ([]Candidate, error) {
	absolute, err := filepath.Abs(selection)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, err
	}
	folder := absolute
	if !info.IsDir() {
		folder = filepath.Dir(absolute)
	}
	var found []Candidate
	seen := map[string]bool{}
	add := func(root, runtime string, p Profile) {
		active, e := files.Join(root, runtime)
		if e != nil {
			return
		}
		if !info.IsDir() && !strings.EqualFold(filepath.Clean(filepath.Join(active, p.Adapter.Executable)), absolute) {
			return
		}
		plan := Plan{Root: root, Profile: p}
		if plan.validateRuntime(active) != nil {
			return
		}
		key := strings.ToLower(filepath.Clean(root) + "|" + filepath.Clean(runtime) + "|" + p.ID)
		if !seen[key] {
			seen[key] = true
			found = append(found, Candidate{Root: root, Runtime: filepath.ToSlash(runtime), Profile: p})
		}
	}
	// A directly selected application-files folder belongs to its parent.
	for _, p := range profiles {
		active := folder
		if !info.IsDir() {
			// An executable may be nested below the application's own directory.
			parent := filepath.Dir(filepath.FromSlash(p.Adapter.Executable))
			if parent != "." {
				for range strings.Split(filepath.ToSlash(parent), "/") {
					active = filepath.Dir(active)
				}
			}
		}
		add(filepath.Dir(active), filepath.Base(active), p)
	}
	if info.IsDir() {
		for _, p := range profiles {
			add(folder, p.Instance.Runtime, p)
		}
		entries, e := os.ReadDir(folder)
		if e != nil {
			return nil, e
		}
		if len(entries) > 512 {
			return nil, fmt.Errorf("this folder has too many entries to inspect; choose the application's own folder")
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.EqualFold(entry.Name(), "util") || strings.EqualFold(entry.Name(), "tmp") {
				continue
			}
			for _, p := range profiles {
				add(folder, entry.Name(), p)
			}
		}
	}
	return found, nil
}
