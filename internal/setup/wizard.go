package setup

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
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
	fmt.Fprintln(out, "Choose an existing container with its application in a separate runtime folder.")
	root, err := ask("1. Container folder (paste or drag the folder here)", "")
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
	runtime, err := ask("3. Runtime directory relative to the container", profile.Instance.Runtime)
	if err != nil {
		return err
	}
	plan, err := Prepare(root, profile.ID, runtime, nil)
	if err != nil {
		return err
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
	plan, err = Prepare(root, profile.ID, runtime, append([]string{}, selected...))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nReview setup\n  Container: %s\n  Runtime: %s\n  Profile: %s\n  Channel: %s\n", plan.Root, plan.Instance.Runtime, profile.ID, plan.Instance.Channel)
	fmt.Fprintf(out, "  Preserve inside runtime: %s\n", strings.Join(profile.Adapter.Preserve, ", "))
	fmt.Fprintf(out, "  Snapshot paths: %s\n", saves)
	fmt.Fprintln(out, "  Add: util/portable-update and updater-update.cmd / updater-rollback.cmd")
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
