package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/release"
)

type Instance struct {
	JSONSchema string   `json:"$schema,omitempty"`
	Schema     int      `json:"schema"`
	Name       string   `json:"name"`
	Adapter    string   `json:"adapter"`
	Runtime    string   `json:"runtime"`
	Channel    string   `json:"channel"`
	Saves      []string `json:"saves"`
}
type Adapter struct {
	JSONSchema   string  `json:"$schema,omitempty"`
	Schema       int     `json:"schema"`
	ID           string  `json:"id"`
	Platform     string  `json:"platform"`
	Architecture string  `json:"architecture"`
	Executable   string  `json:"executable"`
	Package      string  `json:"package,omitempty"`
	Unpack       *Unpack `json:"unpack,omitempty"`
	Version      struct {
		Kind string `json:"kind"`
	} `json:"version"`
	Source          release.Source `json:"source"`
	Preserve        []string       `json:"preserve"`
	RequiredPackage []string       `json:"required_package"`
	RequiredState   []string       `json:"required_state"`
}

type Unpack struct {
	Arguments []string `json:"arguments"`
}

type Config struct {
	Root, Home, Work string
	Instance         Instance
	Adapter          Adapter
}

func decodeFile(path string, target any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(target); e != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), e)
	}
	var trailing any
	if e = d.Decode(&trailing); e != io.EOF {
		return errors.New("extra or invalid trailing JSON data")
	}
	return nil
}
func Load(path string) (*Config, error) {
	absolute, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	home := filepath.Dir(absolute)
	if !strings.EqualFold(filepath.Base(home), "portable-update") || !strings.EqualFold(filepath.Base(filepath.Dir(home)), "util") {
		return nil, errors.New("configuration must be stored under util/portable-update")
	}
	root := filepath.Dir(filepath.Dir(home))
	work, e := files.Join(root, "tmp/portable-update")
	if e != nil {
		return nil, e
	}
	if _, e = files.Join(root, "util/portable-update"); e != nil {
		return nil, e
	}
	var instance Instance
	if e = decodeFile(absolute, &instance); e != nil {
		return nil, e
	}
	if instance.Schema != 2 {
		return nil, errors.New("unsupported container configuration schema")
	}
	if instance.Channel != "nightly" && instance.Channel != "stable" && instance.Channel != "latest" {
		return nil, errors.New("channel must be stable, nightly or latest")
	}
	adapterPath, e := files.Join(home, instance.Adapter)
	if e != nil {
		return nil, e
	}
	var adapter Adapter
	if e = decodeFile(adapterPath, &adapter); e != nil {
		return nil, e
	}
	if adapter.Schema != 1 {
		return nil, errors.New("unsupported adapter schema")
	}
	if adapter.Platform != runtime.GOOS || adapter.Architecture != runtime.GOARCH {
		return nil, errors.New("adapter platform does not match this executable")
	}
	if adapter.Version.Kind != "windows-file-version" && adapter.Version.Kind != "artifact-sha256" {
		return nil, errors.New("unsupported version detector")
	}
	if adapter.Package != "" && adapter.Package != "archive" && adapter.Package != "executable-unpack" {
		return nil, errors.New("unsupported package format")
	}
	if adapter.Package == "executable-unpack" {
		if adapter.Source.Checksum == "when-published" {
			return nil, errors.New("executable unpack requires a publisher checksum")
		}
		if adapter.Unpack == nil || len(adapter.Unpack.Arguments) == 0 || len(adapter.Unpack.Arguments) > 32 {
			return nil, errors.New("executable unpack requires an argument list")
		}
		destinations := 0
		for _, arg := range adapter.Unpack.Arguments {
			destinations += strings.Count(arg, "{destination}")
			if len(arg) > 2048 || strings.ContainsAny(strings.ReplaceAll(arg, "{destination}", ""), "\x00{}") {
				return nil, errors.New("invalid executable unpack argument")
			}
		}
		if destinations != 1 {
			return nil, errors.New("unpack arguments must contain exactly one {destination} placeholder")
		}
	} else if adapter.Unpack != nil {
		return nil, errors.New("unpack arguments require the executable-unpack package format")
	}
	if e = release.ValidateSource(adapter.Source); e != nil {
		return nil, e
	}
	if adapter.Source.Feed != nil && adapter.Source.Feed.Channels[instance.Channel] == "" {
		return nil, errors.New("adapter does not define the configured channel")
	}
	if !release.Supports(adapter.Source.Provider) {
		return nil, errors.New("unsupported release provider")
	}
	if adapter.Source.Repository != "" && !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(adapter.Source.Repository) {
		return nil, errors.New("invalid source repository")
	}
	if adapter.Source.Checksum != "" && adapter.Source.Checksum != "required" && adapter.Source.Checksum != "when-published" {
		return nil, errors.New("unsupported checksum policy")
	}
	if adapter.Source.Provider == "forgejo" && adapter.Source.Host != "https://codeberg.org" {
		return nil, errors.New("unsupported Forgejo host")
	}
	if _, e = regexp.Compile(adapter.Source.AssetPattern); e != nil {
		return nil, e
	}
	if strings.EqualFold(instance.Runtime, "util") || strings.HasPrefix(strings.ToLower(filepath.ToSlash(instance.Runtime)), "util/") || strings.EqualFold(instance.Runtime, "tmp") || strings.HasPrefix(strings.ToLower(filepath.ToSlash(instance.Runtime)), "tmp/") {
		return nil, errors.New("runtime overlaps updater")
	}
	if _, e = files.Join(root, instance.Runtime); e != nil {
		return nil, e
	}
	for _, p := range append(append(append([]string{adapter.Executable}, adapter.Preserve...), adapter.RequiredPackage...), adapter.RequiredState...) {
		if _, e = files.Relative(p); e != nil {
			return nil, e
		}
	}
	for _, p := range instance.Saves {
		target, e := files.Join(root, p)
		if e != nil {
			return nil, e
		}
		if files.Within(home, target) || files.Within(target, home) || files.Within(work, target) || files.Within(target, work) {
			return nil, errors.New("save snapshot overlaps updater storage")
		}
	}
	// Overlapping preservation roots make replacement ambiguous; reject them.
	for i, a := range adapter.Preserve {
		if files.Within(strings.ToLower(filepath.Clean(filepath.FromSlash(a))), strings.ToLower(filepath.Clean(filepath.FromSlash(adapter.Executable)))) {
			return nil, errors.New("preservation path contains the runtime entry point")
		}
		for j, b := range adapter.Preserve {
			if i != j && files.Within(strings.ToLower(filepath.Clean(a)), strings.ToLower(filepath.Clean(b))) {
				return nil, errors.New("overlapping preservation paths")
			}
		}
	}
	return &Config{Root: root, Home: home, Work: work, Instance: instance, Adapter: adapter}, nil
}
