package updater

import (
	"context"
	"debug/pe"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/portable-updater/internal/download"
	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/platform"
	"github.com/keshon/portable-updater/internal/release"
)

func (p *Engine) validateCandidate(path string, expected Version) error {
	if e := files.ValidateTree(path); e != nil {
		return e
	}
	for _, rel := range p.Adapter.RequiredPackage {
		target, e := files.Join(path, rel)
		if e != nil {
			return e
		}
		if !files.Exists(target) {
			return fmt.Errorf("downloaded package is incomplete: missing %s", rel)
		}
	}
	if p.Adapter.Version.Kind == "artifact-sha256" {
		f, e := pe.Open(filepath.Join(path, p.policy.Executable))
		if e != nil {
			return fmt.Errorf("candidate is not a Windows executable: %w", e)
		}
		defer f.Close()
		if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
			return errors.New("candidate is not Windows x64")
		}
		return nil
	}
	value, e := executableVersion(filepath.Join(path, p.policy.Executable))
	if e != nil {
		return e
	}
	version, e := parseVersion(value)
	if e != nil {
		return e
	}
	if compareVersion(version, expected) != 0 {
		return errors.New("downloaded executable version does not match its release tag")
	}
	return nil
}

type Receipt struct {
	Adapter         string        `json:"adapter"`
	Tag             string        `json:"tag"`
	Asset           release.Asset `json:"asset"`
	ExecutableHash  string        `json:"executable_hash"`
	InstalledAt     string        `json:"installed_at"`
	PreviousRuntime string        `json:"previous_runtime"`
}

func (p *Engine) writeReceipt(selected release.Selection, hash string) error {
	return jsonFile(filepath.Join(p.home, "state/installed.json"), Receipt{Adapter: p.Adapter.ID, Tag: selected.Tag, Asset: selected.Asset, ExecutableHash: hash, InstalledAt: files.Stamp(), PreviousRuntime: p.state.Previous})
}
func sameRelease(a, b release.Asset) bool {
	return a.URL == b.URL && a.Size == b.Size && a.Digest == b.Digest && a.ID == b.ID && a.UpdatedAt == b.UpdatedAt
}

func (p *Engine) candidateMatchesInstalled(candidate string) (bool, error) {
	match := true
	err := filepath.WalkDir(candidate, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(candidate, path)
		if err != nil {
			return err
		}
		for _, keep := range p.policy.Preserve {
			if files.Within(strings.ToLower(filepath.Clean(keep)), strings.ToLower(rel)) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if entry.IsDir() {
			return nil
		}
		target, err := files.Join(p.runtime(), rel)
		if err != nil {
			return err
		}
		if !files.Exists(target) {
			match = false
			return nil
		}
		a, err := files.Digest(path)
		if err != nil {
			return err
		}
		b, err := files.Digest(target)
		if err != nil {
			return err
		}
		if a != b {
			match = false
		}
		return nil
	})
	return match, err
}
func (p *Engine) Update(ctx context.Context, display io.Writer) error {
	fmt.Fprintf(display, "%s\nChecking for updates...\n", p.policy.Name)
	if p.state.Phase == "swapping" || p.state.Phase == "old_moved" || p.state.Phase == "rolling_back" {
		fmt.Fprintln(display, "Recovering the interrupted update...")
		if e := p.recover(); e != nil {
			return e
		}
	}
	if e := platform.GuardProcesses(p.runtime()); e != nil {
		return e
	}
	for _, r := range p.Adapter.RequiredState {
		target, e := files.Join(p.runtime(), r)
		if e != nil {
			return e
		}
		if !files.Exists(target) {
			return fmt.Errorf("required portable setting is missing: %s; current emulator kept", r)
		}
	}
	executable := filepath.Join(p.runtime(), p.policy.Executable)
	currentHash, e := files.Digest(executable)
	if e != nil {
		return e
	}
	var current, latest Version
	currentString := "installed build"
	numeric := p.Adapter.Version.Kind != "artifact-sha256"
	if numeric {
		currentString, e = executableVersion(executable)
		if e != nil {
			return e
		}
		current, e = parseVersion(currentString)
		if e != nil {
			return e
		}
	}
	provider := p.provider
	if provider == nil {
		provider, e = release.Lookup(p.Adapter.Source.Provider)
		if e != nil {
			return e
		}
	}
	selected, e := provider.Latest(ctx, p.Adapter.Source, p.Instance.Channel)
	if e != nil {
		return e
	}
	if numeric {
		latest, e = parseVersion(selected.Tag)
		if e != nil {
			return e
		}
		switch compareVersion(current, latest) {
		case 0:
			fmt.Fprintf(display, "Already current (%s).\n", currentString)
			return nil
		case 1:
			fmt.Fprintf(display, "Installed version %s is newer than %s. Kept the installed emulator.\n", currentString, selected.Tag)
			return nil
		}
	}
	var receipt Receipt
	if b, e := os.ReadFile(filepath.Join(p.home, "state/installed.json")); e == nil && json.Unmarshal(b, &receipt) == nil && receipt.Adapter == p.Adapter.ID && receipt.Tag == selected.Tag && sameRelease(receipt.Asset, selected.Asset) && receipt.ExecutableHash == currentHash {
		fmt.Fprintf(display, "Already current (%s).\n", selected.Tag)
		return nil
	}
	fmt.Fprintf(display, "Updating %s -> %s (%s).\n", currentString, selected.Tag, p.Instance.Channel)
	fmt.Fprintf(p.output(), "Integrity policy: %s; published digest: %s\n", p.Adapter.Source.Checksum, selected.Asset.Digest)
	path, e := download.CachePath(p.work, selected.Asset)
	if e != nil {
		return e
	}
	if e = download.Fetch(ctx, selected.Asset, path, display); e != nil {
		return e
	}
	if e = platform.GuardProcesses(p.runtime()); e != nil {
		return e
	}
	if p.state.Phase == "staged" {
		p.state.Phase = "abandoned"
		if e = p.save(); e != nil {
			return e
		}
	}
	fmt.Fprintln(display, "Verifying and preparing the emulator...")
	previousJournal := p.state
	if e = p.stage(path); e != nil {
		return e
	}
	candidate, e := files.Join(p.root, p.state.Stage)
	if e != nil {
		return e
	}
	if e = p.validateCandidate(candidate, latest); e != nil {
		return e
	}
	candidateHash, e := files.Digest(filepath.Join(candidate, p.policy.Executable))
	if e != nil {
		return e
	}
	matches := false
	if !numeric && candidateHash == currentHash {
		matches, e = p.candidateMatchesInstalled(candidate)
		if e != nil {
			return e
		}
	}
	if matches {
		p.state.Phase = "verified_current"
		if previousJournal.Phase == "applied" {
			p.state = previousJournal
		}
		if e = p.save(); e != nil {
			return e
		}
		if e = p.writeReceipt(selected, currentHash); e != nil {
			return e
		}
		fmt.Fprintf(display, "Already current (%s).\n", selected.Tag)
		return nil
	}
	fmt.Fprintln(display, "Backing up saves and preserving settings...")
	if e = p.apply(false); e != nil {
		return e
	}
	if e = p.writeReceipt(selected, candidateHash); e != nil {
		fmt.Fprintln(display, "Updated successfully; installation receipt could not be written. See update.log.")
	}
	fmt.Fprintf(display, "Updated to %s. Settings and saves preserved.\n", selected.Tag)
	return nil
}
func (p *Engine) Restore(display io.Writer) error {
	fmt.Fprintf(display, "%s\nRestoring the previous emulator...\n", p.policy.Name)
	if p.state.Phase == "swapping" || p.state.Phase == "old_moved" || p.state.Phase == "rolling_back" {
		if e := p.recover(); e != nil {
			return e
		}
	}
	if e := p.rollback(); e != nil {
		return e
	}
	if p.Adapter.Version.Kind == "artifact-sha256" {
		fmt.Fprintln(display, "Restored the previous emulator. External saves kept at their current progress.")
		return nil
	}
	value, e := executableVersion(filepath.Join(p.runtime(), p.policy.Executable))
	if e != nil {
		return e
	}
	fmt.Fprintf(display, "Restored %s. External saves kept at their current progress.\n", value)
	return nil
}
