// Package selfupdate replaces only the installed updater and unchanged bundled
// definitions. Application configuration, runtime and state are never targets.
package selfupdate

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"debug/pe"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	bundle "github.com/keshon/portable-updater"
	"github.com/keshon/portable-updater/internal/download"
	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/platform"
	"github.com/keshon/portable-updater/internal/release"
)

var stableVersion = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)

type replacement struct {
	Path   string
	Before string
	After  string
}

type plan struct {
	Version string
	Files   []replacement
}

func homeFor(executable string) (string, string, error) {
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return "", "", err
	}
	home := filepath.Dir(absolute)
	if !strings.EqualFold(filepath.Base(home), "portable-update") || !strings.EqualFold(filepath.Base(filepath.Dir(home)), "util") || !strings.EqualFold(filepath.Base(absolute), "portable-update.exe") {
		return "", "", errors.New("self-update is available for installed copies under util/portable-update; run setup first")
	}
	root := filepath.Dir(filepath.Dir(home))
	if _, err = files.Join(root, "util/portable-update/portable-update.exe"); err != nil {
		return "", "", err
	}
	return root, home, nil
}

// Start verifies a stable official release before handing off to a temporary
// copy of the current updater. The helper waits for this process's lock and
// executable to be released; no shell or installed runtime is involved.
func Start(ctx context.Context, executable, current string, out io.Writer) error {
	root, home, err := homeFor(executable)
	if err != nil {
		return err
	}
	unlock, err := platform.LockUpdater(filepath.Join(home, "updater.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	fmt.Fprintln(out, "Checking for Portable Updater releases...")
	source := release.Source{Provider: "github", Repository: "keshon/portable-updater", AssetPattern: `^portable-updater-v[0-9]+\.[0-9]+\.[0-9]+-windows-x64\.zip$`}
	selected, err := (release.GitHubProvider{}).Latest(ctx, source, "stable")
	if err != nil {
		return fmt.Errorf("could not find a published Portable Updater release: %w", err)
	}
	if !stableVersion.MatchString(selected.Tag) || selected.Asset.Name != "portable-updater-"+selected.Tag+"-windows-x64.zip" {
		return errors.New("unexpected Portable Updater release name")
	}
	if selected.Asset.Size > 64<<20 {
		return errors.New("updater archive exceeds 64 MiB")
	}
	if !newer(selected.Tag, current) {
		fmt.Fprintln(out, "Portable Updater is already current ("+current+").")
		return nil
	}
	work, err := files.Join(root, "tmp/portable-update/self-update")
	if err != nil {
		return err
	}
	stage := filepath.Join(work, files.Stamp())
	if err = os.MkdirAll(stage, 0755); err != nil {
		return err
	}
	archive := filepath.Join(stage, selected.Asset.Name)
	if err = download.Fetch(ctx, selected.Asset, archive, out); err != nil {
		return err
	}
	packageDir := filepath.Join(stage, "package")
	if err = files.ExtractLimit(archive, packageDir, 128<<20); err != nil {
		return err
	}
	if err = validateBinary(ctx, filepath.Join(packageDir, "portable-update.exe"), selected.Tag); err != nil {
		return err
	}
	p, skipped, err := prepare(home, packageDir, selected.Tag)
	if err != nil {
		return err
	}
	for _, name := range skipped {
		fmt.Fprintln(out, "Keeping custom file:", name)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	planPath := filepath.Join(stage, "plan.json")
	if err = os.WriteFile(planPath, data, 0644); err != nil {
		return err
	}
	if err = files.CopyFile(executable, filepath.Join(stage, "helper.exe")); err != nil {
		return err
	}
	command := exec.Command(filepath.Join(stage, "helper.exe"), "--internal-self-update", planPath)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Dir = stage
	fmt.Fprintln(out, "Installing Portable Updater", selected.Tag+". This window will finish the update.")
	fmt.Fprintln(out, "Backup and result:", stage)
	if err = command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

// Unknown development versions may opt into the latest stable release. Stable
// versions never downgrade, even when GitHub marks an older tag as latest.
func newer(latest, current string) bool {
	a, b := stableVersion.FindStringSubmatch(latest), stableVersion.FindStringSubmatch(current)
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	for i := 1; i <= 3; i++ {
		x, y := strings.TrimLeft(a[i], "0"), strings.TrimLeft(b[i], "0")
		if len(x) != len(y) {
			return len(x) > len(y)
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func validateBinary(ctx context.Context, path, version string) error {
	image, err := pe.Open(path)
	if err != nil {
		return err
	}
	machine := image.Machine
	image.Close()
	if machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return errors.New("updater is not Windows x64")
	}
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return err
	}
	if info.Path != "github.com/keshon/portable-updater/cmd/portable-updater" {
		return errors.New("unexpected updater entry point")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(output)) != "Portable Updater "+version {
		return errors.New("downloaded updater version does not match release")
	}
	return nil
}

func managedNames() ([]string, error) {
	names := []string{"LICENSE", "THIRD-PARTY-LICENSES.txt"}
	for _, pattern := range []string{"adapters/*.json", "schemas/*.json"} {
		matches, err := fs.Glob(bundle.Files, pattern)
		if err != nil {
			return nil, err
		}
		names = append(names, matches...)
	}
	return names, nil
}

func prepare(home, packageDir, version string) (plan, []string, error) {
	p := plan{Version: version}
	names, err := managedNames()
	if err != nil {
		return p, nil, err
	}
	// The executable goes last: a returned failure restores the old definitions
	// before the previous executable is put back.
	names = append(names, "portable-update.exe")
	var skipped []string
	for _, name := range names {
		target, err := files.Join(home, name)
		if err != nil {
			return p, nil, err
		}
		old, err := os.ReadFile(target)
		if os.IsNotExist(err) && name != "portable-update.exe" {
			continue
		}
		if err != nil {
			return p, nil, err
		}
		if name != "portable-update.exe" {
			canonical, err := bundle.Files.ReadFile(name)
			if err != nil {
				return p, nil, err
			}
			if !bytes.Equal(old, canonical) {
				skipped = append(skipped, name)
				continue
			}
		}
		candidate := filepath.Join(packageDir, filepath.FromSlash(name))
		data, err := os.ReadFile(candidate)
		if err != nil {
			return p, nil, fmt.Errorf("release is missing %s: %w", name, err)
		}
		if bytes.Equal(old, data) {
			continue
		}
		before, err := files.Digest(target)
		if err != nil {
			return p, nil, err
		}
		after, err := files.Digest(candidate)
		if err != nil {
			return p, nil, err
		}
		p.Files = append(p.Files, replacement{name, before, after})
	}
	return p, skipped, nil
}

// Apply is invoked only by a staged helper. Paths are derived from the helper's
// fixed container workspace, not accepted as arbitrary replacement destinations.
func Apply(planPath string, out io.Writer) (result error) {
	helper, err := os.Executable()
	if err != nil {
		return err
	}
	stage := filepath.Dir(helper)
	if filepath.Base(helper) != "helper.exe" || filepath.Clean(planPath) != filepath.Join(stage, "plan.json") {
		return errors.New("invalid self-update helper location")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(stage))))
	expected, err := files.Join(root, "tmp/portable-update/self-update/"+filepath.Base(stage))
	if err != nil || !strings.EqualFold(expected, stage) {
		return errors.New("invalid self-update workspace")
	}
	log, err := os.OpenFile(filepath.Join(stage, "result.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer log.Close()
	out = io.MultiWriter(out, log)
	defer func() {
		if result != nil {
			fmt.Fprintln(out, "Self-update failed:", result)
		}
	}()
	data, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	var p plan
	if err = json.Unmarshal(data, &p); err != nil {
		return err
	}
	if !stableVersion.MatchString(p.Version) {
		return errors.New("invalid self-update version")
	}
	home, err := files.Join(root, "util/portable-update")
	if err != nil {
		return err
	}
	var unlock func()
	deadline := time.Now().Add(30 * time.Second)
	for {
		unlock, err = platform.LockUpdater(filepath.Join(home, "updater.lock"))
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer unlock()
	return applyFiles(home, stage, p, out)
}

func applyFiles(home, stage string, p plan, out io.Writer) (result error) {
	names, err := managedNames()
	if err != nil {
		return err
	}
	allowed := map[string]bool{"portable-update.exe": true}
	for _, name := range names {
		allowed[name] = true
	}
	seen := map[string]bool{}
	// Check every path and both hashes before the first mutation.
	for _, item := range p.Files {
		if !allowed[item.Path] || seen[item.Path] {
			return errors.New("invalid self-update file list")
		}
		seen[item.Path] = true
		for _, check := range []struct{ base, rel, digest string }{
			{home, item.Path, item.Before}, {stage, "package/" + item.Path, item.After},
		} {
			path, err := files.Join(check.base, check.rel)
			if err != nil {
				return err
			}
			digest, err := files.Digest(path)
			if err != nil {
				return err
			}
			if digest != check.digest {
				return fmt.Errorf("self-update file changed: %s", item.Path)
			}
		}
	}
	var moved []string
	defer func() {
		if result == nil {
			return
		}
		for i := len(moved) - 1; i >= 0; i-- {
			name := moved[i]
			target := filepath.Join(home, filepath.FromSlash(name))
			backup := filepath.Join(stage, "previous", filepath.FromSlash(name))
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				result = errors.Join(result, err)
				continue
			}
			result = errors.Join(result, os.Rename(backup, target))
		}
	}()
	for _, item := range p.Files {
		target := filepath.Join(home, filepath.FromSlash(item.Path))
		backup, err := files.Join(stage, "previous/"+item.Path)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(backup), 0755); err != nil {
			return err
		}
		if files.Exists(backup) {
			return errors.New("self-update backup already exists; refusing to overwrite it")
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			err = os.Rename(target, backup)
			if err == nil || time.Now().After(deadline) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if err != nil {
			return err
		}
		moved = append(moved, item.Path)
		if err = files.CopyFile(filepath.Join(stage, "package", filepath.FromSlash(item.Path)), target); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "Portable Updater updated to", p.Version+".")
	fmt.Fprintln(out, "Your application, configuration and update history were kept.")
	return nil
}
