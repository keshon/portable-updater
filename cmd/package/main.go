// Command package creates a distributable ZIP from an already-built updater.
package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	bundle "github.com/keshon/portable-updater"
)

func main() {
	version := flag.String("version", "", "release version, such as v0.1.0")
	binary := flag.String("binary", "portable-update.exe", "built Windows x64 executable")
	out := flag.String("out", "dist", "output directory")
	flag.Parse()
	if err := validateReleaseBinary(*version, *binary); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := pack(*version, *binary, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func validateReleaseBinary(version, binary string) error {
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return fmt.Errorf("release binary has no Go build information: %w", err)
	}
	if info.Path != "github.com/keshon/portable-updater/cmd/portable-updater" {
		return fmt.Errorf("release binary has the wrong entry point: %s", info.Path)
	}
	absolute, err := filepath.Abs(binary)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, absolute, "--version").Output()
	if err != nil {
		return fmt.Errorf("could not read release binary version: %w", err)
	}
	want := "Portable Updater " + version
	if strings.TrimSpace(string(output)) != want {
		return fmt.Errorf("release binary version must be %q, got %q", want, strings.TrimSpace(string(output)))
	}
	return nil
}

func pack(version, binary, out string) (err error) {
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$`).MatchString(version) {
		return fmt.Errorf("invalid release version %q", version)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(out, 0755); err != nil {
		return err
	}
	name := "portable-updater-" + version + "-windows-x64.zip"
	path := filepath.Join(out, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = f.Close()
			if cleanupErr := os.Remove(path); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
				err = fmt.Errorf("%w; partial archive cleanup failed: %v", err, cleanupErr)
			}
		}
	}()
	w := zip.NewWriter(f)
	contents := map[string][]byte{"portable-update.exe": data, "START-HERE.txt": []byte("Portable Updater " + version + "\r\n\r\nExtract this ZIP into a temporary folder, then double-click portable-update.exe.\r\nThe setup wizard connects the updater to an existing portable application.\r\nReview the runtime and data paths before confirming. No application update runs during setup.\r\n\r\nAfter setup, use updater-update.cmd and updater-rollback.cmd in the application folder.\r\nDocumentation: https://github.com/keshon/portable-updater#readme\r\n")}
	for _, name := range []string{"LICENSE", "THIRD-PARTY-LICENSES.txt"} {
		b, e := bundle.Files.ReadFile(name)
		if e != nil {
			w.Close()
			f.Close()
			return e
		}
		contents[name] = b
	}
	for _, entry := range []string{"portable-update.exe", "START-HERE.txt", "LICENSE", "THIRD-PARTY-LICENSES.txt"} {
		header := &zip.FileHeader{Name: entry, Method: zip.Deflate}
		header.SetMode(0644)
		dest, e := w.CreateHeader(header)
		if e != nil {
			w.Close()
			f.Close()
			return e
		}
		if _, e = dest.Write(contents[entry]); e != nil {
			w.Close()
			f.Close()
			return e
		}
	}
	if err = w.Close(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	archive, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(archive)
	if err = os.WriteFile(filepath.Join(out, "SHA256SUMS.txt"), []byte(fmt.Sprintf("%x  %s\n", digest, name)), 0644); err != nil {
		return err
	}
	notes := "Portable Updater " + version + " for Windows x64.\n\nDownload and extract the ZIP, then double-click `portable-update.exe` to configure an existing portable application. All profiles and setup files are bundled; no Go, Python, PowerShell or 7-Zip installation is needed to run the updater.\n\nAfter setup, use the update and rollback commands in the application folder. See [setup and operating instructions](https://github.com/keshon/portable-updater/blob/main/docs/running.md) for data preservation and recovery behavior.\n\n`SHA256SUMS.txt` contains the archive checksum. Target applications retain their own system requirements.\n"
	if err = os.WriteFile(filepath.Join(out, "RELEASE-NOTES.md"), []byte(notes), 0644); err != nil {
		return err
	}
	fmt.Println(path)
	complete = true
	return nil
}
