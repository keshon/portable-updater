package main

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageIncludesExecutableLicensesAndMatchingChecksum(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "portable-update.exe")
	if err := os.WriteFile(binary, []byte("fixture executable"), 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "dist")
	if err := pack("v0.1.0", binary, out); err != nil {
		t.Fatal(err)
	}
	name := "portable-updater-v0.1.0-windows-x64.zip"
	archive, err := os.ReadFile(filepath.Join(out, name))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	sums, err := os.ReadFile(filepath.Join(out, "SHA256SUMS.txt"))
	if err != nil || string(sums) != fmt.Sprintf("%x  %s\n", digest, name) {
		t.Fatal("wrong package checksum")
	}
	r, err := zip.OpenReader(filepath.Join(out, name))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	seen := map[string]bool{}
	for _, f := range r.File {
		seen[f.Name] = true
	}
	for _, name := range []string{"portable-update.exe", "LICENSE", "THIRD-PARTY-LICENSES.txt", "START-HERE.txt"} {
		if !seen[name] {
			t.Fatalf("missing %s", name)
		}
	}
	if len(seen) != 4 {
		t.Fatal("unexpected distribution contents")
	}
	if err := pack("v0.1.0", binary, out); err == nil {
		t.Fatal("replaced an existing release archive")
	}
	if err := pack("../escape", binary, out); err == nil || !strings.Contains(err.Error(), "invalid release version") {
		t.Fatal("invalid release version accepted")
	}
}

func TestPackagingFailureRemovesOnlyNewArchive(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "portable-update.exe")
	if err := os.WriteFile(binary, []byte("fixture executable"), 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "dist")
	if err := os.MkdirAll(filepath.Join(out, "SHA256SUMS.txt"), 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(out, "SHA256SUMS.txt/keep")
	if err := os.WriteFile(marker, []byte("unrelated file"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := pack("v0.1.0", binary, out); err == nil {
		t.Fatal("checksum write failure ignored")
	}
	if _, err := os.Stat(filepath.Join(out, "portable-updater-v0.1.0-windows-x64.zip")); !os.IsNotExist(err) {
		t.Fatal("partial archive retained")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("unrelated file removed")
	}
}

func TestReleaseRejectsUnversionedOrWrongBinary(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fake.exe")
	if err := os.WriteFile(p, []byte("not a release binary"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseBinary("v0.1.0", p); err == nil {
		t.Fatal("non-Go binary accepted")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseBinary("v0.1.0", executable); err == nil {
		t.Fatal("test executable accepted as updater")
	}
}
