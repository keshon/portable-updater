package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func Stamp() string { return time.Now().UTC().Format("20060102T150405.000000000Z") }

func Exists(p string) bool { _, e := os.Lstat(p); return e == nil }

func Within(base, p string) bool {
	r, e := filepath.Rel(base, p)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(os.PathSeparator)) && !filepath.IsAbs(r)
}

// Canonical resolves existing ancestors, including Windows short-name aliases.
// Missing descendants are appended so destinations can be checked before creation.
func Canonical(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var missing []string
	for current := absolute; ; current = filepath.Dir(current) {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) || filepath.Dir(current) == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
	}
}

// Also reject Windows aliases/ADS, even when this source is tested elsewhere.

func Relative(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" || strings.ContainsAny(p, ":\x00") || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("unsafe Relative path: %q", p)
	}
	for _, s := range strings.Split(strings.TrimSuffix(p, "/"), "/") {
		if s == "" || s == "." || s == ".." || strings.TrimRight(s, " .") != s {
			return "", fmt.Errorf("unsafe path component: %q", p)
		}
		first := strings.ToUpper(strings.SplitN(s, ".", 2)[0])
		if first == "CON" || first == "PRN" || first == "AUX" || first == "NUL" || regexp.MustCompile(`^(COM|LPT)[0-9]$`).MatchString(first) {
			return "", fmt.Errorf("reserved path: %q", p)
		}
	}
	return filepath.FromSlash(strings.TrimSuffix(p, "/")), nil
}

func Join(base, rel string) (string, error) {
	r, e := Relative(rel)
	if e != nil {
		return "", e
	}
	p := filepath.Join(base, r)
	if !Within(base, p) {
		return "", errors.New("path escapes container")
	}
	// Reject links/junctions in all existing ancestors, including base itself.
	for q := p; ; q = filepath.Dir(q) {
		st, e := os.Lstat(q)
		if e == nil && st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("linked path refused: %s", q)
		}
		if e != nil && !os.IsNotExist(e) {
			return "", e
		}
		if filepath.Dir(q) == q {
			break
		}
	}
	return p, nil
}
