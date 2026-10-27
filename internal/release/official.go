package release

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/keshon/portable-updater/internal/httpx"
	"github.com/keshon/portable-updater/internal/platform"
)

type ForgejoProvider struct{}

func (ForgejoProvider) Latest(ctx context.Context, source Source, channel string) (Selection, error) {
	endpoint := source.Host + "/api/v1/repos/" + source.Repository + "/releases"
	var releases []Release
	if channel == "stable" || channel == "latest" {
		var release Release
		if e := metadata(ctx, endpoint+"/latest", &release); e != nil {
			return Selection{}, e
		}
		releases = []Release{release}
	} else {
		if e := metadata(ctx, endpoint+"?limit=30", &releases); e != nil {
			return Selection{}, e
		}
	}
	return selectRelease(releases, source, channel)
}

type RPCS3Provider struct{}

func (RPCS3Provider) Latest(ctx context.Context, source Source, channel string) (Selection, error) {
	var document struct {
		Code   int `json:"return_code"`
		Latest struct {
			Version string `json:"version"`
			Windows struct {
				Download string `json:"download"`
				Size     int64  `json:"size"`
				Checksum string `json:"checksum"`
			} `json:"windows"`
		} `json:"latest_build"`
	}
	endpoint := "https://update.rpcs3.net/?api=v3&os_type=windows&os_arch=x64&os_version=" + platform.OSVersion()
	if e := metadata(ctx, endpoint, &document); e != nil {
		return Selection{}, e
	}
	w := document.Latest.Windows
	if document.Code < 0 || w.Download == "" {
		return Selection{}, errors.New("RPCS3 update service did not return a Windows build")
	}
	if !strings.HasPrefix(w.Download, "https://github.com/RPCS3/rpcs3-binaries-win/releases/download/") {
		return Selection{}, errors.New("unexpected RPCS3 package source")
	}
	checksum := strings.ToLower(w.Checksum)
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(checksum) {
		return Selection{}, errors.New("RPCS3 checksum is missing or invalid")
	}
	parsed, e := url.Parse(w.Download)
	if e != nil {
		return Selection{}, e
	}
	return Selection{Tag: document.Latest.Version, Asset: Asset{Name: path.Base(parsed.Path), URL: w.Download, Size: w.Size, Digest: "sha256:" + checksum}}, nil
}

type DolphinProvider struct{}

func (DolphinProvider) Latest(ctx context.Context, source Source, channel string) (Selection, error) {
	track := "beta"
	if channel == "nightly" {
		track = "dev"
	}
	var document struct {
		Version   string `json:"shortrev"`
		Hash      string `json:"hash"`
		Artifacts []struct {
			System string `json:"system"`
			URL    string `json:"url"`
		} `json:"artifacts"`
	}
	if e := metadata(ctx, "https://dolphin-emu.org/update/latest/"+track, &document); e != nil {
		return Selection{}, e
	}
	for _, item := range document.Artifacts {
		if item.System != "Windows x64" {
			continue
		}
		if !strings.HasPrefix(item.URL, "https://dl.dolphin-emu.org/") {
			return Selection{}, errors.New("unexpected Dolphin package source")
		}
		req, e := http.NewRequestWithContext(ctx, "HEAD", item.URL, nil)
		if e != nil {
			return Selection{}, e
		}
		response, e := httpx.Client.Do(req)
		if e != nil {
			return Selection{}, e
		}
		response.Body.Close()
		if response.StatusCode != 200 || response.ContentLength <= 0 {
			return Selection{}, fmt.Errorf("Dolphin package size unavailable (HTTP %d)", response.StatusCode)
		}
		parsed, _ := url.Parse(item.URL)
		return Selection{Tag: document.Version, Asset: Asset{Name: path.Base(parsed.Path), URL: item.URL, Size: response.ContentLength, UpdatedAt: document.Hash}}, nil
	}
	return Selection{}, errors.New("Dolphin did not publish a Windows x64 package")
}
