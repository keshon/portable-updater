package setup

import (
	"bytes"
	"debug/pe"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	bundle "github.com/keshon/portable-updater"
	"github.com/keshon/portable-updater/internal/config"
	"github.com/keshon/portable-updater/internal/files"
)

type Profile struct {
	ID          string
	Instance    config.Instance
	Adapter     config.Adapter
	AdapterData []byte
}

func Profiles() ([]Profile, error) {
	names, err := fs.Glob(bundle.Files, "examples/*.json")
	if err != nil {
		return nil, err
	}
	var profiles []Profile
	for _, name := range names {
		var p Profile
		p.ID = strings.TrimSuffix(filepath.Base(name), ".json")
		data, err := bundle.Files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &p.Instance); err != nil {
			return nil, err
		}
		p.AdapterData, err = bundle.Files.ReadFile(p.Instance.Adapter)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(p.AdapterData, &p.Adapter); err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

func Detect(root string, profiles []Profile) []int {
	var matches []int
	for i, p := range profiles {
		path, err := files.Join(root, filepath.ToSlash(filepath.Join(p.Instance.Runtime, p.Adapter.Executable)))
		if err == nil && files.Exists(path) {
			matches = append(matches, i)
		}
	}
	return matches
}

type Plan struct {
	Root     string
	Profile  Profile
	Instance config.Instance
	assets   map[string][]byte
}

// ErrSetupConflict identifies installation conflicts, not runtime path mistakes.
var ErrSetupConflict = errors.New("setup cannot reuse existing updater files")

// Prepare reads the installation and constructs a reviewable plan without writing.
// Nil saves selects existing example paths; an empty slice selects no snapshots.
func Prepare(root, profileID, runtime string, saves []string) (*Plan, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("container must be an existing directory")
	}
	profiles, err := Profiles()
	if err != nil {
		return nil, err
	}
	var selected *Profile
	for i := range profiles {
		if profiles[i].ID == profileID {
			selected = &profiles[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("unknown profile: %s", profileID)
	}
	p := &Plan{Root: root, Profile: *selected, Instance: selected.Instance, assets: map[string][]byte{}}
	if runtime != "" {
		p.Instance.Runtime = filepath.ToSlash(runtime)
	}
	normalized := strings.ToLower(filepath.ToSlash(p.Instance.Runtime))
	if normalized == "util" || strings.HasPrefix(normalized, "util/") || normalized == "tmp" || strings.HasPrefix(normalized, "tmp/") {
		return nil, errors.New("runtime overlaps updater storage")
	}
	p.Instance.Name = filepath.Base(root)
	p.Instance.JSONSchema = "schemas/instance.schema.json"
	active, err := files.Join(root, p.Instance.Runtime)
	if err != nil {
		return nil, err
	}
	if err = files.ValidateTree(active); err != nil {
		return nil, err
	}
	if err = p.validateRuntime(active); err != nil {
		return nil, err
	}
	if saves == nil {
		for _, rel := range selected.Instance.Saves {
			path, err := files.Join(root, rel)
			if err != nil {
				return nil, err
			}
			if files.Exists(path) {
				saves = append(saves, rel)
			}
		}
	}
	p.Instance.Saves = append([]string{}, saves...)
	for _, rel := range p.Instance.Saves {
		target, err := files.Join(root, rel)
		if err != nil {
			return nil, err
		}
		for _, reserved := range []string{"util/portable-update", "tmp/portable-update"} {
			boundary, err := files.Join(root, reserved)
			if err != nil {
				return nil, err
			}
			if files.Within(boundary, target) || files.Within(target, boundary) {
				return nil, errors.New("snapshot path overlaps updater storage")
			}
		}
	}
	data, err := json.MarshalIndent(p.Instance, "", "  ")
	if err != nil {
		return nil, err
	}
	p.assets["util/portable-update/update.json"] = append(data, '\n')
	p.assets["util/portable-update/"+p.Instance.Adapter] = p.Profile.AdapterData
	for _, name := range []string{"LICENSE", "THIRD-PARTY-LICENSES.txt", "schemas/adapter.schema.json", "schemas/instance.schema.json"} {
		data, err := bundle.Files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		p.assets["util/portable-update/"+name] = data
	}
	for _, name := range []string{"updater-update.cmd", "updater-rollback.cmd"} {
		data, err := bundle.Files.ReadFile("scripts/launchers/" + name)
		if err != nil {
			return nil, err
		}
		p.assets[name] = []byte(strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n", "\r\n"))
	}
	if err = p.checkDestinations(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Plan) validateRuntime(active string) error {
	image, err := pe.Open(filepath.Join(active, p.Profile.Adapter.Executable))
	if err != nil {
		return fmt.Errorf("runtime entry point is not a Windows executable: %w", err)
	}
	machine := image.Machine
	image.Close()
	if machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return errors.New("runtime must be Windows x64")
	}
	for _, rel := range append(append([]string{}, p.Profile.Adapter.RequiredPackage...), p.Profile.Adapter.RequiredState...) {
		path, err := files.Join(active, rel)
		if err != nil {
			return err
		}
		if !files.Exists(path) {
			return fmt.Errorf("runtime is missing %s; check the runtime directory and portable configuration", rel)
		}
	}
	return nil
}

func (p *Plan) checkDestinations() error {
	home, err := files.Join(p.Root, "util/portable-update")
	if err != nil {
		return err
	}
	if files.Exists(home) {
		if err := files.ValidateTree(home); err != nil {
			return fmt.Errorf("%w: %v", ErrSetupConflict, err)
		}
		if files.Exists(filepath.Join(home, "state")) {
			return fmt.Errorf("%w: existing updater state has no configuration; move it to a backup before a fresh setup", ErrSetupConflict)
		}
	}
	for _, rel := range p.Paths() {
		path, err := files.Join(p.Root, rel)
		if err != nil {
			return err
		}
		if data, err := os.ReadFile(path); err == nil {
			if rel == "util/portable-update/update.json" || !bytes.Equal(data, p.assets[rel]) {
				return fmt.Errorf("%w: %s already exists with different or configured contents; setup will not overwrite it", ErrSetupConflict, rel)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("%w: %v", ErrSetupConflict, err)
		}
	}
	return nil
}

func (p *Plan) Paths() []string {
	paths := make([]string, 0, len(p.assets))
	for rel := range p.assets {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	return paths
}

var openSetupFile = os.OpenFile

// Install never replaces an existing file. On error it removes only the files
// and empty directories created by this invocation, leaving application data alone.
func (p *Plan) Install(executable string) (err error) {
	if err = p.checkDestinations(); err != nil {
		return err
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		return err
	}
	assets := make(map[string][]byte, len(p.assets)+1)
	for k, v := range p.assets {
		assets[k] = v
	}
	assets["util/portable-update/portable-update.exe"] = data
	// Validate the existing binary before writing anything. Setup may run from
	// its final directory; an identical file can be retained without replacing it.
	binaryPath, err := files.Join(p.Root, "util/portable-update/portable-update.exe")
	if err != nil {
		return err
	}
	if existing, e := os.ReadFile(binaryPath); e == nil {
		if !bytes.Equal(existing, data) {
			return fmt.Errorf("%w: existing portable-update.exe differs from this setup executable", ErrSetupConflict)
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	var createdFiles, createdDirs []string
	defer func() {
		if err == nil {
			return
		}
		for i := len(createdFiles) - 1; i >= 0; i-- {
			if cleanupErr := os.Remove(createdFiles[i]); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
				err = fmt.Errorf("%w; cleanup incomplete: %v", err, cleanupErr)
			}
		}
		for i := len(createdDirs) - 1; i >= 0; i-- {
			if cleanupErr := os.Remove(createdDirs[i]); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
				err = fmt.Errorf("%w; cleanup incomplete: %v", err, cleanupErr)
			}
		}
	}()
	paths := p.Paths()
	paths = append(paths, "util/portable-update/portable-update.exe")
	for _, rel := range paths {
		path, e := files.Join(p.Root, rel)
		if e != nil {
			return e
		}
		if existing, e := os.ReadFile(path); e == nil {
			if rel == "util/portable-update/update.json" || !bytes.Equal(existing, assets[rel]) {
				return fmt.Errorf("%w: %s changed during setup; nothing will be overwritten", ErrSetupConflict, rel)
			}
			continue
		} else if !os.IsNotExist(e) {
			return e
		}
		var missing []string
		for dir := filepath.Dir(path); !files.Exists(dir); dir = filepath.Dir(dir) {
			missing = append(missing, dir)
		}
		for i := len(missing) - 1; i >= 0; i-- {
			if e = os.Mkdir(missing[i], 0755); e != nil {
				return e
			}
			createdDirs = append(createdDirs, missing[i])
		}
		f, e := openSetupFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
		if e != nil {
			return e
		}
		createdFiles = append(createdFiles, path)
		_, e = f.Write(assets[rel])
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	_, err = config.Load(filepath.Join(p.Root, "util/portable-update/update.json"))
	return err
}
