package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func CopyFile(src, dst string) error {
	if e := os.MkdirAll(filepath.Dir(dst), 0755); e != nil {
		return e
	}
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	ce := out.Close()
	if e != nil {
		return e
	}
	return ce
}

func CopyTree(src, dst string) error {
	resolvedSource, err := Canonical(src)
	if err != nil {
		return err
	}
	resolvedDestination, err := Canonical(dst)
	if err != nil {
		return err
	}
	if Within(resolvedSource, resolvedDestination) || Within(resolvedDestination, resolvedSource) {
		return errors.New("source and destination trees overlap")
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("link refused: %s", p)
		}
		rel, e := filepath.Rel(src, p)
		if e != nil {
			return e
		}
		target := dst
		if rel != "." {
			target, e = Join(dst, rel)
			if e != nil {
				return e
			}
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular file: %s", p)
		}
		return CopyFile(p, target)
	})
}

func ValidateTree(root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("linked runtime entry refused: %s", p)
		}
		if !d.IsDir() {
			i, e := d.Info()
			if e != nil {
				return e
			}
			if !i.Mode().IsRegular() {
				return errors.New("non-regular runtime entry")
			}
		}
		return nil
	})
}

func Size(root string) (int64, error) {
	var n int64
	e := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("save snapshot refuses links")
		}
		if !d.IsDir() {
			i, e := d.Info()
			if e != nil {
				return e
			}
			n += i.Size()
		}
		return nil
	})
	return n, e
}
