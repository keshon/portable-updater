package release

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/keshon/portable-updater/internal/httpx"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mockHTTP(t *testing.T, fn transportFunc) {
	t.Helper()
	previous := httpx.Client
	httpx.Client = &http.Client{Transport: fn}
	t.Cleanup(func() { httpx.Client = previous })
}
func jsonResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func TestRPCS3OfficialFeed(t *testing.T) {
	checksum := strings.Repeat("A", 64)
	mockHTTP(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "update.rpcs3.net" || r.URL.Query().Get("os_arch") != "x64" || r.URL.Query().Get("api") != "v3" {
			t.Fatalf("wrong feed: %s", r.URL)
		}
		return jsonResponse(fmt.Sprintf(`{"return_code":0,"latest_build":{"version":"0.0.43-20410","windows":{"download":"https://github.com/RPCS3/rpcs3-binaries-win/releases/download/build-abc/rpcs3.7z","size":100,"checksum":%q}}}`, checksum)), nil
	})
	got, err := (RPCS3Provider{}).Latest(context.Background(), Source{}, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if got.Tag != "0.0.43-20410" || got.Asset.Digest != "sha256:"+strings.ToLower(checksum) || got.Asset.Size != 100 {
		t.Fatalf("unexpected selection: %+v", got)
	}
}
func TestRPCS3RejectsMissingChecksum(t *testing.T) {
	mockHTTP(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"return_code":0,"latest_build":{"version":"v1","windows":{"download":"https://github.com/RPCS3/rpcs3-binaries-win/releases/download/build-abc/rpcs3.7z","size":100}}}`), nil
	})
	if _, err := (RPCS3Provider{}).Latest(context.Background(), Source{}, "latest"); err == nil {
		t.Fatal("missing checksum accepted")
	}
}
func TestDolphinFeedAndPackageSize(t *testing.T) {
	for _, channel := range []string{"stable", "nightly"} {
		t.Run(channel, func(t *testing.T) {
			calls := 0
			mockHTTP(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method == "HEAD" {
					if r.URL.Host != "dl.dolphin-emu.org" {
						t.Fatalf("wrong package host: %s", r.URL)
					}
					response := jsonResponse("")
					response.ContentLength = 1234
					return response, nil
				}
				suffix := "beta"
				if channel == "nightly" {
					suffix = "dev"
				}
				if r.URL.String() != "https://dolphin-emu.org/update/latest/"+suffix {
					t.Fatalf("wrong channel: %s", r.URL)
				}
				return jsonResponse(`{"shortrev":"2609a","hash":"commit","artifacts":[{"system":"Linux","url":"https://example.invalid/linux"},{"system":"Windows x64","url":"https://dl.dolphin-emu.org/releases/2609a/dolphin-2609a-x64.7z"}]}`), nil
			})
			got, err := (DolphinProvider{}).Latest(context.Background(), Source{}, channel)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 || got.Tag != "2609a" || got.Asset.Size != 1234 || got.Asset.Digest != "" {
				t.Fatalf("unexpected selection: %+v, calls=%d", got, calls)
			}
		})
	}
}
func TestForgejoOfficialAssetWithoutPublishedDigest(t *testing.T) {
	source := Source{Provider: "forgejo", Host: "https://codeberg.org", Repository: "schelling/dosbox-pure-unleashed", AssetPattern: `^dosbox_pure_unleashed-windows-64bit-.*\.zip$`, Checksum: "when-published"}
	mockHTTP(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://codeberg.org/api/v1/repos/schelling/dosbox-pure-unleashed/releases/latest" {
			t.Fatalf("wrong endpoint: %s", r.URL)
		}
		return jsonResponse(`{"tag_name":"1.0-preview7","published_at":"2026-01-01T00:00:00Z","assets":[{"name":"dosbox_pure_unleashed-windows-64bit-1.0-preview7.zip","size":100,"browser_download_url":"https://codeberg.org/schelling/dosbox-pure-unleashed/releases/download/1.0-preview7/dosbox_pure_unleashed-windows-64bit-1.0-preview7.zip"}]}`), nil
	})
	got, err := (ForgejoProvider{}).Latest(context.Background(), source, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if got.Tag != "1.0-preview7" || got.Asset.Digest != "" {
		t.Fatalf("unexpected selection: %+v", got)
	}
	source.Checksum = "required"
	if _, err := (ForgejoProvider{}).Latest(context.Background(), source, "latest"); err == nil {
		t.Fatal("required checksum silently bypassed")
	}
}
