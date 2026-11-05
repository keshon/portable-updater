package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/keshon/portable-updater/internal/config"
)

func Wizard(input io.Reader, out io.Writer, executable string) error {
	scanner := bufio.NewScanner(input)
	ask := func(label, fallback string) (string, error) {
		fmt.Fprint(out, label)
		if fallback != "" {
			fmt.Fprintf(out, " [%s]", fallback)
		}
		fmt.Fprint(out, ": ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", err
			}
			return "", io.EOF
		}
		value := strings.Trim(strings.TrimSpace(scanner.Text()), "\"")
		if value == "" {
			value = fallback
		}
		return value, nil
	}
	fmt.Fprintln(out, "Portable Updater - first-time setup")
	fmt.Fprintln(out, "Keep your portable setup. Update what runs it.")
	fmt.Fprintln(out, "Choose the outer application folder, which contains its runtime, data and utilities.")
	fmt.Fprintln(out, `Example: container C:\Portable\MyApp, runtime C:\Portable\MyApp\runtime.`)
	root, err := ask("1. Container folder (paste or drag the outer folder here)", "")
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	profiles, err := Profiles()
	if err != nil {
		return err
	}
	// Recognize an accidentally selected default runtime, but never infer a
	// different container if the selection already has a configured updater.
	manifestAt := func(folder string) string { return filepath.Join(folder, "util", "portable-update", "update.json") }
	if _, statErr := os.Stat(manifestAt(root)); os.IsNotExist(statErr) && len(Detect(root, profiles)) == 0 {
		parent := filepath.Dir(root)
		for _, index := range Detect(parent, profiles) {
			expected := filepath.Join(parent, filepath.FromSlash(profiles[index].Instance.Runtime))
			if strings.EqualFold(filepath.Clean(root), filepath.Clean(expected)) {
				fmt.Fprintf(out, "You selected the runtime folder. Using its container: %s\n", parent)
				root = parent
				break
			}
		}
	}
	if _, statErr := os.Stat(manifestAt(root)); statErr == nil {
		existing, err := config.Load(manifestAt(root))
		if err != nil {
			return fmt.Errorf("this folder already has an updater configuration that needs attention: %w", err)
		}
		fmt.Fprintf(out, "\nAlready configured: %s\n  Container: %s\n  Runtime: %s\n", existing.Instance.Name, root, existing.Instance.Runtime)
		fmt.Fprintln(out, "Run updater-update.cmd in the container to check for updates. Setup is not needed again.")
		return nil
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	matches := Detect(root, profiles)
	defaultProfile := ""
	if len(matches) == 1 {
		defaultProfile = strconv.Itoa(matches[0] + 1)
	}
	fmt.Fprintln(out, "\nAvailable application profiles:")
	for i, p := range profiles {
		detected := ""
		for _, n := range matches {
			if n == i {
				detected = " (detected)"
			}
		}
		fmt.Fprintf(out, "  %d. %s%s\n", i+1, p.ID, detected)
	}
	choice, err := ask("2. Profile number or name", defaultProfile)
	if err != nil {
		return err
	}
	index := -1
	if n, e := strconv.Atoi(choice); e == nil && n > 0 && n <= len(profiles) {
		index = n - 1
	}
	for i, p := range profiles {
		if strings.EqualFold(choice, p.ID) {
			index = i
		}
	}
	if index < 0 {
		return fmt.Errorf("choose one of the listed profiles")
	}
	profile := profiles[index]
	fmt.Fprintf(out, "The runtime is the inner folder containing %s.\n", profile.Adapter.Executable)
	fmt.Fprintf(out, "Press Enter to use %s, or paste its full folder path.\n", filepath.Join(root, profile.Instance.Runtime))
	var plan *Plan
	for {
		runtime, err := ask("3. Runtime folder", profile.Instance.Runtime)
		if err != nil {
			return err
		}
		if filepath.IsAbs(runtime) {
			runtime, err = filepath.Rel(root, runtime)
			if err != nil {
				fmt.Fprintf(out, "Choose a runtime folder inside %s.\n", root)
				continue
			}
		}
		if runtime == "." {
			fmt.Fprintln(out, "That is the container itself. Choose the inner application folder, or press Enter for the default.")
			continue
		}
		plan, err = Prepare(root, profile.ID, runtime, nil)
		if err == nil {
			break
		}
		if errors.Is(err, ErrSetupConflict) {
			return err
		}
		fmt.Fprintf(out, "Cannot use that runtime: %s\nChoose an application folder inside %s, or press Enter for the default.\n", err, root)
	}
	defaults := strings.Join(plan.Instance.Saves, ", ")
	if defaults == "" {
		defaults = "-"
	}
	fmt.Fprintln(out, "Snapshot paths back up existing user data; they do not move it or enable portable mode.")
	saves, err := ask("Snapshot paths, comma-separated; '-' for none", defaults)
	if err != nil {
		return err
	}
	var selected []string
	if saves != "-" {
		for _, path := range strings.Split(saves, ",") {
			selected = append(selected, strings.TrimSpace(path))
		}
	}
	plan, err = Prepare(root, profile.ID, plan.Instance.Runtime, append([]string{}, selected...))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nReview setup\n  Container: %s\n  Runtime: %s\n  Profile: %s\n  Channel: %s\n", plan.Root, plan.Instance.Runtime, profile.ID, plan.Instance.Channel)
	fmt.Fprintf(out, "  Preserve inside runtime: %s\n", strings.Join(profile.Adapter.Preserve, ", "))
	fmt.Fprintf(out, "  Snapshot paths: %s\n", saves)
	fmt.Fprintln(out, "  Create configuration and missing updater files under util/portable-update.")
	fmt.Fprintln(out, "  Add or reuse identical updater-update.cmd / updater-rollback.cmd and bundled files.")
	fmt.Fprintln(out, "Check that the application's data paths are configured correctly. Setup does not change them.")
	fmt.Fprintln(out, "Existing application files and launchers will stay in place. No update is started during setup.")
	answer, err := ask("4. Install updater? (yes/no)", "no")
	if err != nil {
		return err
	}
	if !strings.EqualFold(answer, "yes") && !strings.EqualFold(answer, "y") {
		fmt.Fprintln(out, "Setup cancelled. No files were written.")
		return nil
	}
	if err = plan.Install(executable); err != nil {
		return err
	}
	fmt.Fprintln(out, "\nSetup complete. Close the application, then double-click updater-update.cmd.")
	fmt.Fprintln(out, "Use updater-rollback.cmd to restore the previous runtime after an update.")
	fmt.Fprintln(out, "Press Enter to close.")
	scanner.Scan()
	return scanner.Err()
}
