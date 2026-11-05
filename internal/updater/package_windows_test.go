package updater

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/portable-updater/internal/config"
	"github.com/keshon/portable-updater/internal/files"
)

func TestExecutableUnpackValidatesBeforeStaging(t *testing.T) {
	for _, mode := range []string{"success", "success-inline", "incomplete", "failure", "invalid-source"} {
		t.Run(mode, func(t *testing.T) {
			u, _ := fixture(t)
			project := &Engine{transaction: u, Adapter: config.Adapter{Package: "executable-unpack", Unpack: &config.Unpack{Arguments: []string{"--extract-to", "{destination}"}}, RequiredPackage: []string{"emu.exe", "resource.dat"}}}
			project.Adapter.Version.Kind = "artifact-sha256"
			if mode == "success-inline" {
				project.Adapter.Unpack.Arguments = []string{"--extract-to={destination}"}
			}
			source, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			if mode == "invalid-source" {
				source = filepath.Join(t.TempDir(), "invalid.exe")
				put(t, source, "not a PE executable")
			}
			original := runExecutableUnpack
			t.Cleanup(func() { runExecutableUnpack = original })
			called := false
			runExecutableUnpack = func(ctx context.Context, executable, destination, temporary string, arguments []string, out io.Writer) error {
				called = true
				if mode == "success-inline" {
					if len(arguments) != 1 || arguments[0] != "--extract-to="+destination {
						t.Fatal("inline argument not substituted")
					}
				} else if len(arguments) != 2 || arguments[0] != "--extract-to" || arguments[1] != destination {
					t.Fatal("adapter arguments not applied")
				}
				if executable != source || !files.Within(u.work, destination) || !files.Within(u.work, temporary) {
					t.Fatal("unpack escaped workspace")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("missing unpack timeout")
				}
				if mode == "failure" {
					return errors.New("extraction failed")
				}
				b, err := os.ReadFile(source)
				if err != nil {
					return err
				}
				put(t, filepath.Join(destination, "emu.exe"), string(b))
				if strings.HasPrefix(mode, "success") {
					put(t, filepath.Join(destination, "resource.dat"), "resource")
				}
				return nil
			}
			before, err := files.Digest(filepath.Join(u.runtime(), "emu.exe"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := project.preparePackage(context.Background(), source, Version{})
			if strings.HasPrefix(mode, "success") {
				if err != nil || !files.Exists(filepath.Join(result, "resource.dat")) {
					t.Fatalf("unpack failed: %v", err)
				}
			} else if err == nil {
				t.Fatal("failed/incomplete extraction accepted")
			}
			if mode == "invalid-source" && called {
				t.Fatal("invalid executable was run")
			}
			after, _ := files.Digest(filepath.Join(u.runtime(), "emu.exe"))
			if before != after || u.state.Phase != "" {
				t.Fatal("preparation changed installed runtime")
			}
		})
	}
}
