package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/httpx"
)

type Source struct {
	Provider     string `json:"provider"`
	Repository   string `json:"repository"`
	AssetPattern string `json:"asset_pattern"`
	Host         string `json:"host,omitempty"`
	Checksum     string `json:"checksum,omitempty"`
}
type Asset struct {
	Name      string `json:"name"`
	URL       string `json:"browser_download_url"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	ID        int64  `json:"id,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}
type Release struct {
	Tag        string    `json:"tag_name"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Published  time.Time `json:"published_at"`
	Assets     []Asset   `json:"assets"`
}
type Selection struct {
	Tag   string `json:"tag"`
	Asset Asset  `json:"asset"`
}
type ReleaseProvider interface {
	Latest(context.Context, Source, string) (Selection, error)
}
type GitHubProvider struct{}

var providers = map[string]ReleaseProvider{"github": GitHubProvider{}, "forgejo": ForgejoProvider{}, "rpcs3": RPCS3Provider{}, "dolphin": DolphinProvider{}}

func metadata(ctx context.Context, url string, target any) error {
	req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return e
	}
	req.Header.Set("User-Agent", "PortableUpdaterPilot/0.1")
	req.Header.Set("Accept", "application/vnd.github+json")
	response, e := httpx.Client.Do(req)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("release lookup returned HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(target)
}
func (GitHubProvider) Latest(ctx context.Context, source Source, channel string) (Selection, error) {
	var releases []Release
	url := "https://api.github.com/repos/" + source.Repository + "/releases"
	if channel == "stable" {
		var r Release
		if e := metadata(ctx, url+"/latest", &r); e != nil {
			return Selection{}, e
		}
		releases = []Release{r}
	} else {
		if e := metadata(ctx, url+"?per_page=30", &releases); e != nil {
			return Selection{}, e
		}
	}
	return selectRelease(releases, source, channel)
}
func selectRelease(releases []Release, source Source, channel string) (Selection, error) {
	pattern, e := regexp.Compile(source.AssetPattern)
	if e != nil {
		return Selection{}, e
	}
	sort.Slice(releases, func(i, j int) bool { return releases[i].Published.After(releases[j].Published) })
	for _, r := range releases {
		if r.Draft || (channel == "stable" && r.Prerelease) || (channel == "nightly" && !r.Prerelease) {
			continue
		}
		var assets []Asset
		for _, a := range r.Assets {
			if pattern.MatchString(a.Name) {
				assets = append(assets, a)
			}
		}
		if len(assets) > 1 {
			return Selection{}, errors.New("upstream package selection is ambiguous")
		}
		if len(assets) == 1 {
			a := assets[0]
			if _, e = files.Relative(a.Name); e != nil || strings.ContainsAny(a.Name, "/\\") {
				return Selection{}, errors.New("unsafe upstream filename")
			}
			host := "https://github.com"
			if source.Provider == "forgejo" {
				host = source.Host
			}
			if !strings.HasPrefix(a.URL, host+"/"+source.Repository+"/releases/download/") {
				return Selection{}, errors.New("unexpected upstream download URL")
			}
			if a.Digest == "" && source.Checksum == "when-published" {
				// TLS and the archive decoder remain mandatory; no upstream digest
				// is claimed. The locally computed hash protects subsequent reuse.
			} else if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(a.Digest) {
				return Selection{}, errors.New("upstream SHA-256 is missing; current emulator kept")
			}
			if a.Size <= 0 || a.Size > 2<<30 {
				return Selection{}, errors.New("upstream package size is outside limits")
			}
			return Selection{Tag: r.Tag, Asset: a}, nil
		}
	}
	return Selection{}, errors.New("no portable Windows package found for the configured channel")
}

func Supports(name string) bool { _, ok := providers[name]; return ok }
func Lookup(name string) (ReleaseProvider, error) {
	p, ok := providers[name]
	if !ok {
		return nil, fmt.Errorf("unsupported release provider: %s", name)
	}
	return p, nil
}
