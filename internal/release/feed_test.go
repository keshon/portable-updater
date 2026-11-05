package release

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func sampleFeed() Source {
	return Source{Provider: "json-feed", Feed: &JSONFeed{
		Channels: map[string]string{"stable": "https://updates.example.test/release"},
		Version:  "/version", Asset: "/artifacts", Match: map[string]string{"/platform": "win64"},
		URL: "/url", Size: "/bytes", SHA256: "/digest",
		AllowedDownloadPrefixes: []string{"https://downloads.example.test/packages/"},
	}}
}

func TestJSONFeedRejectsInvalidDocuments(t *testing.T) {
	digest := strings.Repeat("a", 64)
	asset := fmt.Sprintf(`{"platform":"win64","url":"https://downloads.example.test/packages/app.zip","bytes":100,"digest":%q}`, digest)
	cases := map[string]string{
		"ambiguous":           `{"version":"v1","artifacts":[` + asset + `,` + asset + `]}`,
		"no match":            `{"version":"v1","artifacts":[]}`,
		"version absent":      `{"artifacts":[` + asset + `]}`,
		"wrong version type":  `{"version":4,"artifacts":[` + asset + `]}`,
		"missing checksum":    strings.Replace(asset, `"digest":`, `"not_digest":`, 1),
		"bad checksum":        strings.Replace(asset, digest, "bad", 1),
		"different host":      strings.Replace(asset, "downloads.example.test/packages/", "downloads.example.test.evil/packages/", 1),
		"different directory": strings.Replace(asset, "/packages/", "/packages-evil/", 1),
		"credentials":         strings.Replace(asset, "https://downloads", "https://user@downloads", 1),
		"insecure":            strings.Replace(asset, "https://downloads", "http://downloads", 1),
		"zero size":           strings.Replace(asset, `"bytes":100`, `"bytes":0`, 1),
		"oversize":            strings.Replace(asset, `"bytes":100`, `"bytes":2147483649`, 1),
		"fraction":            strings.Replace(asset, `"bytes":100`, `"bytes":100.5`, 1),
		"string size":         strings.Replace(asset, `"bytes":100`, `"bytes":"100"`, 1),
		"oversized integer":   strings.Replace(asset, `"bytes":100`, `"bytes":9223372036854775808`, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if strings.HasPrefix(body, `{"platform"`) {
				body = `{"version":"v1","artifacts":[` + body + `]}`
			}
			mockHTTP(t, func(r *http.Request) (*http.Response, error) { return jsonResponse(body), nil })
			if _, err := (JSONFeedProvider{}).Latest(context.Background(), sampleFeed(), "stable"); err == nil {
				t.Fatal("invalid feed accepted")
			}
		})
	}
}

func TestJSONFeedDefinitionValidation(t *testing.T) {
	cases := map[string]func(*Source){
		"no feed":             func(s *Source) { s.Feed = nil },
		"wrong provider":      func(s *Source) { s.Provider = "github" },
		"bad channel":         func(s *Source) { s.Feed.Channels["beta"] = "https://example.test/" },
		"http endpoint":       func(s *Source) { s.Feed.Channels["stable"] = "http://example.test/" },
		"unknown placeholder": func(s *Source) { s.Feed.Channels["stable"] = "https://example.test/{shell}" },
		"invalid pointer":     func(s *Source) { s.Feed.Version = "$.version" },
		"invalid escape":      func(s *Source) { s.Feed.Match = map[string]string{"/bad~2": "x"} },
		"no size":             func(s *Source) { s.Feed.Size = "" },
		"two sizes":           func(s *Source) { s.Feed.SizeFromHead = true },
		"no digest":           func(s *Source) { s.Feed.SHA256 = "" },
		"no allowlist":        func(s *Source) { s.Feed.AllowedDownloadPrefixes = nil },
		"host prefix":         func(s *Source) { s.Feed.AllowedDownloadPrefixes = []string{"https://example.test"} },
		"query prefix":        func(s *Source) { s.Feed.AllowedDownloadPrefixes = []string{"https://example.test/?q=/"} },
		"repository fields":   func(s *Source) { s.Repository = "owner/repo" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := sampleFeed()
			change(&s)
			if err := ValidateSource(s); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
}

func TestJSONFeedPointerEscapes(t *testing.T) {
	value := map[string]any{"a/b": []any{map[string]any{"~key": "ok"}}}
	got, ok := pointer(value, "/a~1b/0/~0key")
	if !ok || got != "ok" {
		t.Fatalf("got %v, %v", got, ok)
	}
	for _, p := range []string{"/a~1b/01", "/a~1b/-1", "/a~1b/1", "/a~1b/0/~2key"} {
		if _, ok := pointer(value, p); ok {
			t.Fatalf("invalid pointer resolved: %s", p)
		}
	}
}

func TestJSONFeedUnsupportedChannelDoesNotRequest(t *testing.T) {
	mockHTTP(t, func(r *http.Request) (*http.Response, error) { t.Fatal("unexpected request"); return nil, nil })
	if _, err := (JSONFeedProvider{}).Latest(context.Background(), sampleFeed(), "nightly"); err == nil {
		t.Fatal("undefined channel accepted")
	}
}

// Explicit opt-in keeps network availability out of ordinary CI tests.
func TestJSONFeedOfficialLive(t *testing.T) {
	if os.Getenv("PORTABLE_UPDATER_LIVE_TEST") != "1" {
		t.Skip("live release lookup disabled")
	}
	for _, app := range []string{"rpcs3", "dolphin"} {
		for _, channel := range []string{"stable", "latest", "nightly"} {
			t.Run(app+"/"+channel, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				selection, err := (JSONFeedProvider{}).Latest(ctx, adapterSource(t, app), channel)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("%s: %s (%d bytes), publisher checksum: %t", selection.Tag, selection.Asset.Name, selection.Asset.Size, selection.Asset.Digest != "")
			})
		}
	}
}

func TestJSONFeedHEADFailures(t *testing.T) {
	for _, test := range []struct {
		status int
		size   int64
	}{{500, 100}, {200, -1}, {200, 0}, {200, 2147483649}} {
		t.Run(fmt.Sprintf("%d_%d", test.status, test.size), func(t *testing.T) {
			s := sampleFeed()
			s.Feed.Size = ""
			s.Feed.SizeFromHead = true
			mockHTTP(t, func(r *http.Request) (*http.Response, error) {
				if r.Method == "HEAD" {
					response := jsonResponse("")
					response.StatusCode = test.status
					response.ContentLength = test.size
					return response, nil
				}
				return jsonResponse(fmt.Sprintf(`{"version":"v1","artifacts":[{"platform":"win64","url":"https://downloads.example.test/packages/app.zip","digest":%q}]}`, strings.Repeat("a", 64))), nil
			})
			if _, err := (JSONFeedProvider{}).Latest(context.Background(), s, "stable"); err == nil {
				t.Fatal("invalid HEAD size accepted")
			}
		})
	}
}
