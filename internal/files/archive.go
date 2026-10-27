package files

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
)

func extractFile(dst, name string, mode fs.FileMode, open func() (io.ReadCloser, error), seen map[string]bool) error {
	p, e := Join(dst, name)
	if e != nil {
		return e
	}
	key := strings.ToLower(filepath.Clean(p))
	if seen[key] {
		return fmt.Errorf("duplicate archive path: %s", name)
	}
	seen[key] = true
	if mode.IsDir() {
		return os.MkdirAll(p, 0755)
	}
	if !mode.IsRegular() {
		return fmt.Errorf("archive link/special file refused: %s", name)
	}
	if e = os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		return e
	}
	r, e := open()
	if e != nil {
		return e
	}
	defer r.Close()
	w, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	// A single extracted file cannot exceed 4 GiB. Emulator packages need far less.
	n, e := io.Copy(w, io.LimitReader(r, (4<<30)+1))
	ce := w.Close()
	if e != nil {
		return e
	}
	if n > 4<<30 {
		return errors.New("archive file exceeds size limit")
	}
	return ce
}

func Extract(src, dst string) error {
	seen := map[string]bool{}
	switch strings.ToLower(filepath.Ext(src)) {
	case ".zip":
		r, e := zip.OpenReader(src)
		if e != nil {
			return e
		}
		defer r.Close()
		if len(r.File) > 50000 {
			return errors.New("too many archive entries")
		}
		var total uint64
		for _, f := range r.File {
			total += f.UncompressedSize64
			if total > 2<<30 {
				return errors.New("unpacked package exceeds 2 GiB")
			}
		}
		for _, f := range r.File {
			if e = extractFile(dst, f.Name, f.Mode(), f.Open, seen); e != nil {
				return e
			}
		}
	case ".7z":
		r, e := sevenzip.OpenReader(src)
		if e != nil {
			return e
		}
		defer r.Close()
		if len(r.File) > 50000 {
			return errors.New("too many archive entries")
		}
		var total uint64
		for _, f := range r.File {
			n := f.FileInfo().Size()
			if n < 0 {
				return errors.New("invalid archive size")
			}
			total += uint64(n)
			if total > 2<<30 {
				return errors.New("unpacked package exceeds 2 GiB")
			}
		}
		for _, f := range r.File {
			if e = extractFile(dst, f.Name, f.Mode(), f.Open, seen); e != nil {
				return e
			}
		}
	default:
		return errors.New("use an extracted directory, .zip or .7z portable package")
	}
	return nil
}
