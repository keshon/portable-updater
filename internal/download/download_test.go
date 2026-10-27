package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/release"
)

func TestParallelRangesAssembledInOrder(t *testing.T) {
	data := []byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	var calls atomic.Int32
	server := testNetwork(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var start, end int
		fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		time.Sleep(time.Duration((start/4)%4) * time.Millisecond)
		serveRange(w, r, data)
	})
	downloadWorkers = 8
	p := filepath.Join(t.TempDir(), "package.7z")
	if e := Fetch(context.Background(), testAsset(data, server.URL), p, io.Discard); e != nil {
		t.Fatal(e)
	}
	if read(t, p) != string(data) || calls.Load() < 8 {
		t.Fatal("parallel archive order/content incorrect")
	}
	if files.Exists(p + ".chunks") {
		t.Fatal("verified temporary range files not cleaned")
	}
}

func TestPartialRangeContinuesFromReceivedBytes(t *testing.T) {
	data := []byte("01234567")
	first := true
	server := testNetwork(t, func(w http.ResponseWriter, r *http.Request) {
		if first {
			first = false
			w.Header().Set("Content-Range", "bytes 0-3/8")
			w.Header().Set("Content-Length", "4")
			w.WriteHeader(206)
			w.Write(data[:2])
			return
		}
		serveRange(w, r, data)
	})
	p := filepath.Join(t.TempDir(), "package.7z")
	if e := Fetch(context.Background(), testAsset(data, server.URL), p, io.Discard); e != nil {
		t.Fatal(e)
	}
	if read(t, p) != string(data) {
		t.Fatal("partial response prefix was not resumed correctly")
	}
}

func testAsset(data []byte, url string) release.Asset {
	h := sha256.Sum256(data)
	return release.Asset{Name: "package.7z", URL: url, Digest: "sha256:" + hex.EncodeToString(h[:]), Size: int64(len(data))}
}

func testNetwork(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	testClient := server.Client()
	oldChunk := downloadChunkBytes
	oldFactory := downloadClientFactory
	oldWorkers := downloadWorkers
	downloadWorkers = 1
	downloadClientFactory = func() *http.Client { return testClient }

	testClient.Timeout = time.Second
	downloadChunkBytes = 4
	t.Cleanup(func() {

		downloadChunkBytes = oldChunk
		downloadClientFactory = oldFactory
		downloadWorkers = oldWorkers
		server.Close()
	})
	return server
}

func serveRange(w http.ResponseWriter, r *http.Request, data []byte) {
	var a, b int
	fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &a, &b)
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, b, len(data)))
	w.Header().Set("Content-Length", strconv.Itoa(b-a+1))
	w.WriteHeader(206)
	w.Write(data[a : b+1])
}

func TestDownloadResumeRetryAndChecksum(t *testing.T) {
	data := []byte("0123456789abcdef")
	attempts := 0
	server := testNetwork(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(503)
			return
		}
		serveRange(w, r, data)
	})
	asset := testAsset(data, server.URL)
	p := filepath.Join(t.TempDir(), "package.7z")
	put(t, filepath.Join(p+".chunks", chunkName(downloadRange{0, 3})+".chunk"), string(data[:4]))
	if e := Fetch(context.Background(), asset, p, io.Discard); e != nil {
		t.Fatal(e)
	}
	if read(t, p) != string(data) || attempts != 4 {
		t.Fatalf("resume/retry failed; attempts=%d", attempts)
	}
	if e := Fetch(context.Background(), asset, p, io.Discard); e != nil {
		t.Fatal(e)
	}
	if attempts != 4 {
		t.Fatal("verified cache downloaded again")
	}
}

func TestDownloadRejectsWrongRangeAndChecksum(t *testing.T) {
	data := []byte("01234567")
	t.Run("wrong range", func(t *testing.T) {
		server := testNetwork(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Range", "bytes 0-3/999")
			w.WriteHeader(206)
			w.Write([]byte("0123"))
		})
		p := filepath.Join(t.TempDir(), "package.7z")
		if e := Fetch(context.Background(), testAsset(data, server.URL), p, io.Discard); e == nil {
			t.Fatal("wrong range accepted")
		}
		if files.Exists(p) {
			t.Fatal("failed download marked complete")
		}
	})
	t.Run("wrong digest", func(t *testing.T) {
		server := testNetwork(t, func(w http.ResponseWriter, r *http.Request) { serveRange(w, r, []byte("abcdefgh")) })
		p := filepath.Join(t.TempDir(), "package.7z")
		if e := Fetch(context.Background(), testAsset(data, server.URL), p, io.Discard); e == nil {
			t.Fatal("wrong digest accepted")
		}
		if files.Exists(p) {
			t.Fatal("bad digest marked complete")
		}
	})
}

func TestStalledDownloadIsBounded(t *testing.T) {
	server := testNetwork(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-3/4")
		w.WriteHeader(206)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	downloadClientFactory().Timeout = 50 * time.Millisecond
	p := filepath.Join(t.TempDir(), "package.7z")
	start := time.Now()
	if e := Fetch(context.Background(), testAsset([]byte("0123"), server.URL), p, io.Discard); e == nil {
		t.Fatal("stall accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("stall exceeded retry bound")
	}
	if files.Exists(p) {
		t.Fatal("incomplete download accepted")
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
