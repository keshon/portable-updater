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
	"github.com/keshon/portable-updater/internal/console"
	"github.com/keshon/portable-updater/internal/files"
)

var errCancel = errors.New("setup cancelled")
var errDone = errors.New("setup finished")
var errBack = errors.New("back to previous screen")

type wizard struct {
	input      *bufio.Scanner
	out        *console.Writer
	executable string
	profiles   []Profile
}

func Wizard(input io.Reader, out io.Writer, executable string) error {
	profiles, err := Profiles()
	if err != nil {
		return err
	}
	w := wizard{input: bufio.NewScanner(input), out: console.New(out), executable: executable, profiles: profiles}
	w.input.Buffer(make([]byte, 4096), 64*1024)
	w.out.Heading("Portable Updater - first-time setup")
	initial := ""
	home := filepath.Dir(executable)
	if strings.EqualFold(filepath.Base(home), "portable-update") && strings.EqualFold(filepath.Base(filepath.Dir(home)), "util") {
		root := filepath.Dir(filepath.Dir(home))
		matches, e := Discover(root, profiles)
		if e == nil && len(matches) > 0 {
			initial = root
		}
	}
	err = w.run(initial)
	if errors.Is(err, errCancel) {
		w.out.Warning("Setup cancelled. No files were written.")
		return nil
	}
	if errors.Is(err, errDone) {
		return nil
	}
	return err
}

func (w *wizard) read(prompt string) (string, error) {
	fmt.Fprint(w.out, prompt)
	if !w.input.Scan() {
		if err := w.input.Err(); err != nil {
			return "", err
		}
		return "", errCancel
	}
	value := strings.Trim(strings.TrimSpace(w.input.Text()), "\"")
	if strings.EqualFold(value, "q") {
		return "", errCancel
	}
	return value, nil
}

func (w *wizard) configured(root string) (bool, error) {
	manifest := filepath.Join(root, "util/portable-update/update.json")
	if _, err := os.Stat(manifest); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return true, err
	}
	c, err := config.Load(manifest)
	if err != nil {
		return true, fmt.Errorf("existing updater configuration needs attention: %w", err)
	}
	w.out.Success("\nAlready configured: %s", c.Instance.Name)
	fmt.Fprintf(w.out, "Application folder  %s\nApplication files   %s\n\nTo update: double-click updater-update.cmd\n", root, c.Instance.Runtime)
	for {
		w.out.Action("Enter  Done    A  Set up another application    Q  Quit")
		answer, err := w.read("> ")
		if err != nil {
			return true, err
		}
		if answer == "" {
			return true, errDone
		}
		if strings.EqualFold(answer, "a") {
			return true, nil
		}
		w.out.Warning("Press Enter to finish, A for another application, or Q to quit.")
	}
}

func (w *wizard) locate(initial string) (Candidate, error) {
	selection := initial
	for {
		if selection == "" {
			fmt.Fprintln(w.out, "\nChoose the application folder or its executable. Paste or drag it here.")
			value, err := w.read("Application location (Q cancels): ")
			if err != nil {
				return Candidate{}, err
			}
			if value == "" {
				w.out.Warning("Enter a folder or executable path.")
				continue
			}
			selection = value
		}
		absolute, err := filepath.Abs(selection)
		if err != nil {
			w.out.Error("Cannot use that location: %s", err)
			selection = ""
			continue
		}
		if done, err := w.configured(absolute); done {
			if err != nil {
				return Candidate{}, err
			}
			selection = ""
			continue
		}
		matches, err := Discover(absolute, w.profiles)
		if err != nil {
			w.out.Error("Cannot inspect that location: %s", err)
			selection = ""
			continue
		}
		// A selected inner folder or executable may resolve to a configured parent.
		roots := map[string]bool{}
		for _, match := range matches {
			roots[strings.ToLower(match.Root)] = true
		}
		if len(roots) == 1 && len(matches) > 0 && !strings.EqualFold(matches[0].Root, absolute) {
			if done, err := w.configured(matches[0].Root); done {
				if err != nil {
					return Candidate{}, err
				}
				selection = ""
				continue
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			fmt.Fprintln(w.out, "\nMore than one application was found:")
			for i, c := range matches {
				fmt.Fprintf(w.out, "  %d. %s - %s\n", i+1, c.Profile.Name(), filepath.Join(c.Root, c.Runtime))
			}
			for {
				answer, err := w.read("Choose a number, B for another location, or Q to cancel: ")
				if err != nil {
					return Candidate{}, err
				}
				if strings.EqualFold(answer, "b") {
					break
				}
				n, e := strconv.Atoi(answer)
				if e == nil && n > 0 && n <= len(matches) {
					return matches[n-1], nil
				}
				w.out.Warning("Choose one of the listed numbers.")
			}
			selection = ""
			continue
		}
		fmt.Fprintln(w.out, "\nNo supported application was detected at that location.")
		for {
			w.out.Action("P  Choose application manually    B  Choose another location    Q  Cancel")
			answer, err := w.read("> ")
			if err != nil {
				return Candidate{}, err
			}
			if strings.EqualFold(answer, "b") {
				break
			}
			if strings.EqualFold(answer, "p") {
				folder := absolute
				if info, e := os.Stat(folder); e == nil && !info.IsDir() {
					folder = filepath.Dir(folder)
				}
				candidate, e := w.manual(folder)
				if errors.Is(e, errBack) {
					break
				}
				return candidate, e
			}
			w.out.Warning("Choose P, B or Q.")
		}
		selection = ""
	}
}

func (w *wizard) manual(root string) (Candidate, error) {
	fmt.Fprintln(w.out, "\nChoose the application:")
	for i, p := range w.profiles {
		fmt.Fprintf(w.out, "  %d. %s\n", i+1, p.Name())
	}
	var profile Profile
	for {
		answer, err := w.read("Application number (B goes back, Q cancels): ")
		if err != nil {
			return Candidate{}, err
		}
		if strings.EqualFold(answer, "b") {
			return Candidate{}, errBack
		}
		n, e := strconv.Atoi(answer)
		if e == nil && n > 0 && n <= len(w.profiles) {
			profile = w.profiles[n-1]
			break
		}
		w.out.Warning("Choose one of the listed numbers.")
	}
	return w.runtimeFolder(root, profile, profile.Instance.Runtime)
}

func (w *wizard) runtimeFolder(root string, profile Profile, current string) (Candidate, error) {
	for {
		fmt.Fprintf(w.out, "Folder containing %s. Enter uses %s\n", profile.Adapter.Executable, filepath.Join(root, current))
		value, err := w.read("Application files (B goes back): ")
		if err != nil {
			return Candidate{}, err
		}
		if strings.EqualFold(value, "b") {
			return Candidate{}, errBack
		}
		if value == "" {
			value = current
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(root, value)
		}
		candidateRoot := root
		relative, e := filepath.Rel(candidateRoot, value)
		if e == nil && relative == "." {
			w.out.Warning("That is the outer folder. Choose its application-files subfolder.")
			continue
		}
		if e != nil {
			w.out.Warning("Choose a folder inside %s.", root)
			continue
		}
		if _, e = files.Join(candidateRoot, relative); e != nil {
			w.out.Warning("Choose a folder inside %s.", root)
			continue
		}
		plan := Plan{Root: candidateRoot, Profile: profile}
		if e = plan.validateRuntime(value); e != nil {
			w.out.Error("Cannot use those application files: %s", e)
			continue
		}
		return Candidate{Root: candidateRoot, Runtime: filepath.ToSlash(relative), Profile: profile}, nil
	}
}

func (w *wizard) run(initial string) error {
	candidate, err := w.locate(initial)
	if err != nil {
		return err
	}
	plan, err := Prepare(candidate.Root, candidate.Profile.ID, candidate.Runtime, nil)
	if err != nil {
		return err
	}
	for {
		w.review(plan)
		answer, err := w.read("> ")
		if err != nil {
			return err
		}
		switch strings.ToLower(answer) {
		case "":
			if err = plan.Install(w.executable); err != nil {
				return err
			}
			w.out.Success("\n%s updater is ready.", plan.Profile.Name())
			fmt.Fprintf(w.out, "\nApplication folder  %s\n\nTo update:   double-click updater-update.cmd\nTo restore:  double-click updater-rollback.cmd after an update\n", plan.Root)
			fmt.Fprintln(w.out, "\nPress Enter to close.")
			w.input.Scan()
			return w.input.Err()
		case "1", "2", "3", "4", "p", "d":
			updated, e := w.change(plan, strings.ToLower(answer))
			if e != nil {
				return e
			}
			plan = updated
		default:
			w.out.Warning("Press Enter to set up, 1-4 to change a setting, P for application type, D for details, or Q to cancel.")
		}
	}
}

func (w *wizard) review(plan *Plan) {
	w.out.Heading("\nSet up %s", plan.Profile.Name())
	fmt.Fprintln(w.out, "\nCreates configuration and update/restore shortcuts.")
	fmt.Fprintln(w.out, "Reuses matching files. No application update starts during setup.")
	fmt.Fprintln(w.out)
	w.out.Option("1", fmt.Sprintf("%-19s %s", "Application folder", plan.Root))
	w.out.Option("2", fmt.Sprintf("%-19s %s", "Application files", filepath.FromSlash(plan.Instance.Runtime)+"\\"))
	if len(plan.Instance.Saves) == 0 {
		w.out.Option("3", fmt.Sprintf("%-19s %s", "Extra backups", "None selected"))
	} else {
		w.out.Option("3", fmt.Sprintf("%-19s %s", "Extra backups", "Before each update:"))
		for _, path := range plan.Instance.Saves {
			fmt.Fprintf(w.out, "%-7s %-19s %s\n", "", "", filepath.FromSlash(path))
		}
	}
	w.out.Option("4", fmt.Sprintf("%-19s %s", "Release channel", plan.Instance.Channel))
	w.out.Option("P", "Change application type")
	w.out.Option("D", "View preserved settings")
	fmt.Fprintln(w.out)
	w.out.Option("Enter", "Set up updater")
	w.out.Option("Q", "Cancel")
}

func (w *wizard) change(plan *Plan, field string) (*Plan, error) {
	switch field {
	case "1", "2", "p":
		var candidate Candidate
		var err error
		switch field {
		case "1":
			candidate, err = w.locate("")
		case "2":
			candidate, err = w.runtimeFolder(plan.Root, plan.Profile, plan.Instance.Runtime)
		case "p":
			candidate, err = w.manual(plan.Root)
		}
		if errors.Is(err, errBack) {
			return plan, nil
		}
		if err != nil {
			return nil, err
		}
		var saves []string
		keepChoices := field == "2" || (field == "p" && candidate.Profile.ID == plan.Profile.ID)
		if keepChoices {
			saves = append([]string{}, plan.Instance.Saves...)
		}
		updated, err := Prepare(candidate.Root, candidate.Profile.ID, candidate.Runtime, saves)
		if err != nil {
			w.out.Error("Cannot use this setup: %s", err)
			return plan, nil
		}
		if keepChoices {
			updated.Instance.Channel = plan.Instance.Channel
		}
		return updated, nil
	case "3":
		return w.backups(plan)
	case "4":
		for {
			fmt.Fprintf(w.out, "Current release channel: %s\n", plan.Instance.Channel)
			value, err := w.read("Channel: stable, latest or nightly (Enter keeps current, B goes back): ")
			if err != nil {
				return nil, err
			}
			value = strings.ToLower(value)
			if value == "" || value == "b" {
				return plan, nil
			}
			if value != "stable" && value != "latest" && value != "nightly" {
				w.out.Warning("Choose stable, latest or nightly.")
				continue
			}
			if f := plan.Profile.Adapter.Source.Feed; f != nil && f.Channels[value] == "" {
				w.out.Warning("This application does not provide that channel.")
				continue
			}
			plan.Instance.Channel = value
			return plan, nil
		}
	case "d":
		fmt.Fprintln(w.out, "\nSettings copied from the current application-files folder before replacement:")
		if len(plan.Profile.Adapter.Preserve) == 0 {
			fmt.Fprintln(w.out, "None configured. Keep personal data outside the application-files folder.")
		}
		for _, path := range plan.Profile.Adapter.Preserve {
			fmt.Fprintf(w.out, "  %s\n", path)
		}
		fmt.Fprintln(w.out, "Extra backups are separate copies. Restoring the previous application does not restore those copies automatically.")
	}
	return plan, nil
}

func (w *wizard) backups(plan *Plan) (*Plan, error) {
	selected := append([]string{}, plan.Instance.Saves...)
	for {
		w.out.Heading("\nExtra backups before each update")
		if len(selected) == 0 {
			fmt.Fprintln(w.out, "No files or folders selected.")
		}
		for i, path := range selected {
			fmt.Fprintf(w.out, "  %d. %s\n", i+1, path)
		}
		fmt.Fprintln(w.out, "These are extra backup copies. Keep large model and game libraries out of this list.")
		if len(selected) > 0 {
			fmt.Fprintln(w.out, "Paste a file or folder to add it.")
			w.out.Action("R  Remove    Enter  Done    Q  Cancel setup")
		} else {
			fmt.Fprintln(w.out, "Paste a file or folder to add it.")
			w.out.Action("Enter  Done    Q  Cancel setup")
		}
		answer, err := w.read("> ")
		if err != nil {
			return nil, err
		}
		switch strings.ToLower(answer) {
		case "", "b":
			updated, e := Prepare(plan.Root, plan.Profile.ID, plan.Instance.Runtime, selected)
			if e != nil {
				return nil, e
			}
			updated.Instance.Channel = plan.Instance.Channel
			return updated, nil
		default:
			value := answer
			var e error
			if strings.EqualFold(value, "a") {
				value, e = w.read("File or folder (paste or drag; Enter goes back): ")
				if e != nil {
					return nil, e
				}
			}
			if value == "" {
				continue
			}
			if filepath.IsAbs(value) {
				value, e = filepath.Rel(plan.Root, value)
				if e != nil {
					w.out.Error("Choose a path inside %s.", plan.Root)
					continue
				}
			}
			target, e := files.Join(plan.Root, value)
			if e != nil {
				w.out.Error("Choose a path inside %s.", plan.Root)
				continue
			}
			if _, e = os.Stat(target); e != nil {
				w.out.Error("Cannot back up that location: %s", e)
				continue
			}
			next := append(append([]string{}, selected...), filepath.ToSlash(value))
			if _, e = Prepare(plan.Root, plan.Profile.ID, plan.Instance.Runtime, next); e != nil {
				w.out.Error("Cannot add that backup: %s", e)
				continue
			}
			selected = next
		case "r":
			if len(selected) == 0 {
				w.out.Warning("There are no selections to remove.")
				continue
			}
			value, e := w.read("Number to remove (Enter goes back): ")
			if e != nil {
				return nil, e
			}
			if value == "" {
				continue
			}
			n, e := strconv.Atoi(value)
			if e != nil || n < 1 || n > len(selected) {
				w.out.Warning("Choose a listed number.")
				continue
			}
			selected = append(selected[:n-1], selected[n:]...)
		}
	}
}
