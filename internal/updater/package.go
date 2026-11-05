package updater

import (
	"context"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/keshon/portable-updater/internal/files"
)

// Execute only the verified release asset; adapter arguments are passed without a shell.
var runExecutableUnpack = func(ctx context.Context, source, destination, temporary string, arguments []string, output io.Writer) error {
	command := exec.CommandContext(ctx, source, arguments...)
	command.Dir = filepath.Dir(destination)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !strings.EqualFold(key, "TEMP") && !strings.EqualFold(key, "TMP") {
			command.Env = append(command.Env, value)
		}
	}
	command.Env = append(command.Env, "TEMP="+temporary, "TMP="+temporary)
	command.Stdout, command.Stderr = output, output
	return command.Run()
}

func (p *Engine) preparePackage(ctx context.Context, source string, expected Version) (string, error) {
	if p.Adapter.Package == "" || p.Adapter.Package == "archive" {
		return source, nil
	}
	if p.Adapter.Package != "executable-unpack" {
		return "", errors.New("unsupported package format")
	}
	if p.Adapter.Unpack == nil {
		return "", errors.New("executable unpack arguments are missing")
	}
	// Update has verified the release digest before reaching this executable format.
	image, err := pe.Open(source)
	if err != nil {
		return "", fmt.Errorf("unpack source is not a Windows executable: %w", err)
	}
	machine := image.Machine
	image.Close()
	if machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return "", errors.New("unpack source is not Windows x64")
	}
	if err := os.MkdirAll(p.work, 0755); err != nil {
		return "", err
	}
	workspace, err := os.MkdirTemp(p.work, "executable-unpack-")
	if err != nil {
		return "", err
	}
	destination, temporary := filepath.Join(workspace, "runtime"), filepath.Join(workspace, "tmp")
	if err := os.Mkdir(temporary, 0755); err != nil {
		return "", err
	}
	arguments := append([]string{}, p.Adapter.Unpack.Arguments...)
	for i, arg := range arguments {
		arguments[i] = strings.ReplaceAll(arg, "{destination}", destination)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := runExecutableUnpack(ctx, source, destination, temporary, arguments, p.output()); err != nil {
		return "", fmt.Errorf("Executable unpack failed; current runtime kept: %w", err)
	}
	if err := files.ValidateTree(destination); err != nil {
		return "", err
	}
	// Upstream may report an extraction error with exit code zero. Check its output tree.
	if err := p.validateCandidate(destination, expected); err != nil {
		return "", err
	}
	return destination, nil
}
