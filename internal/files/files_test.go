package files

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestZipTraversalAndLinks(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "C:/escape", "sub/../../escape", "CON.txt", "file:stream", "ends. ", "a\\..\\escape"} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			p := filepath.Join(base, "bad.zip")
			f, e := os.Create(p)
			if e != nil {
				t.Fatal(e)
			}
			z := zip.NewWriter(f)
			w, e := z.Create(name)
			if e != nil {
				t.Fatal(e)
			}
			w.Write([]byte("bad"))
			z.Close()
			f.Close()
			if e := Extract(p, filepath.Join(base, "out")); e == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	base := t.TempDir()
	p := filepath.Join(base, "link.zip")
	f, _ := os.Create(p)
	z := zip.NewWriter(f)
	h := &zip.FileHeader{Name: "link"}
	h.SetMode(os.ModeSymlink | 0777)
	w, _ := z.CreateHeader(h)
	w.Write([]byte("../outside"))
	z.Close()
	f.Close()
	if e := Extract(p, filepath.Join(base, "out")); e == nil {
		t.Fatal("symlink accepted")
	}
}
func put(t *testing.T, p, s string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(s), 0644); e != nil {
		t.Fatal(e)
	}
}
func read(t *testing.T, p string) string {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
