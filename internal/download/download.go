package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/httpx"
	"github.com/keshon/portable-updater/internal/release"
)

type downloadRange struct{ start, end int64 }

var downloadWorkers = 8
var downloadChunkBytes int64 = 256 << 10
var downloadClientFactory = httpx.NewClient

func chunkName(r downloadRange) string { return fmt.Sprintf("%010d-%010d", r.start, r.end) }
func fetchChunk(ctx context.Context, client *http.Client, asset release.Asset, r downloadRange, dir string) error {
	complete := filepath.Join(dir, chunkName(r)+".chunk")
	want := r.end - r.start + 1
	if st, e := os.Stat(complete); e == nil && st.Size() == want {
		return nil
	}
	partial := filepath.Join(dir, chunkName(r)+".partial")
	offset := r.start
	if st, e := os.Stat(partial); e == nil {
		if st.Size() > want {
			return errors.New("partial range has an invalid size")
		}
		offset += st.Size()
	}
	file, e := os.OpenFile(partial, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if e != nil {
		return e
	}
	var last error
	for attempt := 0; offset <= r.end && attempt < 3; attempt++ {
		data, err := readRangeWithClient(ctx, client, asset, offset, r.end)
		if len(data) > 0 {
			n, e := file.Write(data)
			if e != nil {
				file.Close()
				return e
			}
			if n != len(data) {
				file.Close()
				return io.ErrShortWrite
			}
			offset += int64(n)
		}
		last = err
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			file.Close()
			return ctx.Err()
		}
	}
	if e = file.Close(); e != nil {
		return e
	}
	if offset != r.end+1 {
		return fmt.Errorf("download stopped after retries; current emulator kept: %w", last)
	}
	return os.Rename(partial, complete)
}
func Fetch(ctx context.Context, asset release.Asset, destination string, progress io.Writer) error {
	if files.Exists(destination) {
		digest, e := files.Digest(destination)
		if e != nil {
			return e
		}
		expected := asset.Digest
		if expected == "" {
			if b, e := os.ReadFile(destination + ".sha256"); e == nil {
				expected = strings.TrimSpace(string(b))
			}
		}
		if expected != "" && digest == expected {
			fmt.Fprintln(progress, "Using the verified download.")
			return nil
		}
		if e = os.Rename(destination, destination+".rejected-"+files.Stamp()); e != nil {
			return e
		}
	}
	if e := os.MkdirAll(filepath.Dir(destination), 0755); e != nil {
		return e
	}
	dir := destination + ".chunks"
	if e := os.MkdirAll(dir, 0755); e != nil {
		return e
	}
	var ranges []downloadRange
	for start := int64(0); start < asset.Size; start += downloadChunkBytes {
		end := start + downloadChunkBytes - 1
		if end >= asset.Size {
			end = asset.Size - 1
		}
		ranges = append(ranges, downloadRange{start, end})
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan downloadRange, len(ranges))
	for _, r := range ranges {
		jobs <- r
	}
	close(jobs)
	type result struct {
		bytes int64
		err   error
	}
	results := make(chan result, downloadWorkers)
	var workers sync.WaitGroup
	for i := 0; i < downloadWorkers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			client := downloadClientFactory()
			defer client.CloseIdleConnections()
			for r := range jobs {
				if ctx.Err() != nil {
					return
				}
				err := fetchChunk(ctx, client, asset, r, dir)
				results <- result{r.end - r.start + 1, err}
				if err != nil {
					return
				}
			}
		}()
	}
	go func() { workers.Wait(); close(results) }()
	var completed int64
	var failure error
	for r := range results {
		if r.err != nil {
			if failure == nil {
				failure = r.err
				cancel()
			}
			continue
		}
		completed += r.bytes
		fmt.Fprintf(progress, "\rDownloading... %d%% (%d / %d MB)", completed*100/asset.Size, completed/(1<<20), (asset.Size+(1<<20)-1)/(1<<20))
	}
	fmt.Fprintln(progress)
	if failure != nil {
		return failure
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	partial := destination + ".partial"
	file, e := os.OpenFile(partial, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if e != nil {
		return e
	}
	h := sha256.New()
	for _, r := range ranges {
		chunk, e := os.Open(filepath.Join(dir, chunkName(r)+".chunk"))
		if e != nil {
			file.Close()
			return e
		}
		_, e = io.Copy(io.MultiWriter(file, h), chunk)
		chunk.Close()
		if e != nil {
			file.Close()
			return e
		}
	}
	if e = file.Sync(); e != nil {
		file.Close()
		return e
	}
	if e = file.Close(); e != nil {
		return e
	}
	digest := "sha256:" + hex.EncodeToString(h.Sum(nil))
	if asset.Digest != "" && digest != asset.Digest {
		_ = os.Rename(partial, partial+".rejected-"+files.Stamp())
		_ = os.Rename(dir, dir+".rejected-"+files.Stamp())
		return errors.New("download checksum mismatch; current emulator kept")
	}
	if e = os.Rename(partial, destination); e != nil {
		return e
	}
	if e = os.WriteFile(destination+".sha256", []byte(digest+"\n"), 0644); e != nil {
		return e
	}
	// Remove only our flat numeric range files after the assembled hash matches.
	// A foreign file or link stops cleanup; no recursive deletion is performed.
	if e = cleanChunkFiles(destination, dir); e != nil {
		fmt.Fprintln(progress, "Download verified; temporary range files retained.")
	}
	return nil
}
func cleanChunkFiles(destination, dir string) error {
	parent, e := filepath.Abs(filepath.Dir(destination))
	if e != nil {
		return e
	}
	absolute, e := filepath.Abs(dir)
	if e != nil {
		return e
	}
	if !files.Within(parent, absolute) || filepath.Base(absolute) != filepath.Base(destination)+".chunks" {
		return errors.New("chunk cleanup outside download directory")
	}
	entries, e := os.ReadDir(absolute)
	if e != nil {
		return e
	}
	pattern := regexp.MustCompile(`^[0-9]{10}-[0-9]{10}\.(chunk|partial)$`)
	for _, entry := range entries {
		if !pattern.MatchString(entry.Name()) || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return errors.New("foreign chunk-cache entry; retained")
		}
	}
	for _, entry := range entries {
		if e = os.Remove(filepath.Join(absolute, entry.Name())); e != nil {
			return e
		}
	}
	return os.Remove(absolute)
}
func readRangeWithClient(ctx context.Context, client *http.Client, asset release.Asset, start, end int64) ([]byte, error) {
	request, e := http.NewRequestWithContext(ctx, "GET", asset.URL, nil)
	if e != nil {
		return nil, e
	}
	request.Header.Set("User-Agent", "PortableUpdaterPilot/0.1")
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	request.Header.Set("Accept-Encoding", "identity")
	response, e := client.Do(request)
	if e != nil {
		return nil, e
	}
	defer response.Body.Close()
	want := end - start + 1
	if response.StatusCode == http.StatusPartialContent {
		expected := fmt.Sprintf("bytes %d-%d/%d", start, end, asset.Size)
		if response.Header.Get("Content-Range") != expected {
			return nil, errors.New("server returned an unexpected byte range")
		}
	} else if response.StatusCode == 200 && start == 0 && want == asset.Size {
		// Small packages also work with servers which ignore the Range header.
	} else {
		return nil, fmt.Errorf("download server returned HTTP %d without the requested range", response.StatusCode)
	}
	if value := response.Header.Get("Content-Length"); value != "" {
		n, e := strconv.ParseInt(value, 10, 64)
		if e != nil || n != want {
			return nil, errors.New("download chunk length does not match")
		}
	}
	if value := response.Header.Get("Content-Encoding"); value != "" && value != "identity" {
		return nil, errors.New("encoded byte range refused")
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, want+1))
	if e != nil {
		return data, e
	}
	if int64(len(data)) != want {
		return data, io.ErrUnexpectedEOF
	}
	return data, nil
}

func CachePath(work string, asset release.Asset) (string, error) {
	if asset.Digest == "" {
		identity := fmt.Sprintf("%s|%d|%d|%s", asset.URL, asset.Size, asset.ID, asset.UpdatedAt)
		digest := sha256.Sum256([]byte(identity))
		return files.Join(work, "cache/"+hex.EncodeToString(digest[:])+"/"+asset.Name)
	}
	if !strings.HasPrefix(asset.Digest, "sha256:") || len(asset.Digest) != 71 {
		return "", errors.New("invalid download digest")
	}
	return files.Join(work, "cache/"+strings.TrimPrefix(asset.Digest, "sha256:")+"/"+asset.Name)
}
