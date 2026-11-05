package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/httpx"
	"github.com/keshon/portable-updater/internal/platform"
)

// JSONFeed describes a single release document. Selectors are JSON Pointers;
// version and updated_at are document-relative, asset fields are asset-relative.
type JSONFeed struct {
	Channels                map[string]string `json:"channels"`
	Version                 string            `json:"version"`
	UpdatedAt               string            `json:"updated_at,omitempty"`
	Asset                   string            `json:"asset"`
	Match                   map[string]string `json:"match,omitempty"`
	URL                     string            `json:"url"`
	Size                    string            `json:"size,omitempty"`
	SizeFromHead            bool              `json:"size_from_head,omitempty"`
	SHA256                  string            `json:"sha256,omitempty"`
	AllowedDownloadPrefixes []string          `json:"allowed_download_prefixes"`
}

type JSONFeedProvider struct{}

func httpsURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

func validPointer(p string) bool {
	if p == "" || !strings.HasPrefix(p, "/") {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] == '~' {
			if i+1 >= len(p) || (p[i+1] != '0' && p[i+1] != '1') {
				return false
			}
			i++
		}
	}
	return true
}

// ValidateSource keeps malformed feed definitions from reaching the network.
func ValidateSource(s Source) error {
	if s.Provider != "json-feed" {
		if s.Feed != nil {
			return errors.New("feed configuration requires json-feed provider")
		}
		return nil
	}
	f := s.Feed
	if f == nil || len(f.Channels) == 0 || len(f.Channels) > 3 {
		return errors.New("JSON feed requires channel endpoints")
	}
	if f.SHA256 == "" && s.Checksum != "when-published" {
		return errors.New("feed requires a publisher SHA-256 field")
	}
	if s.Repository != "" || s.Host != "" || s.AssetPattern != "" {
		return errors.New("JSON feed cannot contain repository source fields")
	}
	for channel, endpoint := range f.Channels {
		if channel != "stable" && channel != "latest" && channel != "nightly" {
			return errors.New("unsupported feed channel")
		}
		expanded := strings.ReplaceAll(endpoint, "{os_version}", "10.0.0")
		if len(endpoint) > 4096 || strings.ContainsAny(expanded, "{}") || !httpsURL(expanded) {
			return errors.New("invalid HTTPS feed endpoint or placeholder")
		}
	}
	if !validPointer(f.Version) || !validPointer(f.Asset) || !validPointer(f.URL) {
		return errors.New("feed requires version, asset and URL JSON Pointers")
	}
	for _, p := range []string{f.UpdatedAt, f.Size, f.SHA256} {
		if p != "" && !validPointer(p) {
			return errors.New("invalid feed field JSON Pointer")
		}
	}
	if (f.Size != "") == f.SizeFromHead {
		return errors.New("feed requires exactly one size source: pointer or HEAD")
	}
	if len(f.Match) > 16 {
		return errors.New("too many feed selection predicates")
	}
	for p := range f.Match {
		if !validPointer(p) {
			return errors.New("invalid feed match JSON Pointer")
		}
	}
	if len(f.AllowedDownloadPrefixes) == 0 || len(f.AllowedDownloadPrefixes) > 16 {
		return errors.New("feed requires download URL prefixes")
	}
	for _, prefix := range f.AllowedDownloadPrefixes {
		u, err := url.Parse(prefix)
		if err != nil || !httpsURL(prefix) || u.RawQuery != "" || !strings.HasSuffix(prefix, "/") {
			return errors.New("download prefixes must be HTTPS directory URLs")
		}
	}
	return nil
}

func pointer(value any, selector string) (any, bool) {
	if !validPointer(selector) {
		return nil, false
	}
	for _, part := range strings.Split(selector[1:], "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch v := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = v[part]
			if !ok {
				return nil, false
			}
		case []any:
			i, e := strconv.Atoi(part)
			if e != nil || i < 0 || i >= len(v) || strconv.Itoa(i) != part {
				return nil, false
			}
			value = v[i]
		default:
			return nil, false
		}
	}
	return value, true
}

func field(value any, selector string) (string, error) {
	v, ok := pointer(value, selector)
	s, isString := v.(string)
	if !ok || !isString || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("missing or invalid feed string at %s", selector)
	}
	return s, nil
}

func (JSONFeedProvider) Latest(ctx context.Context, s Source, channel string) (Selection, error) {
	if s.Provider != "json-feed" {
		return Selection{}, errors.New("JSON feed provider requires a json-feed source")
	}
	if err := ValidateSource(s); err != nil {
		return Selection{}, err
	}
	f := s.Feed
	endpoint, ok := f.Channels[channel]
	if !ok {
		return Selection{}, fmt.Errorf("feed does not define channel %q", channel)
	}
	endpoint = strings.ReplaceAll(endpoint, "{os_version}", url.QueryEscape(platform.OSVersion()))
	var document any
	if err := metadata(ctx, endpoint, &document); err != nil {
		return Selection{}, err
	}
	tag, err := field(document, f.Version)
	if err != nil {
		return Selection{}, err
	}
	selected, ok := pointer(document, f.Asset)
	if !ok {
		return Selection{}, errors.New("feed asset selector did not resolve")
	}
	candidates, array := selected.([]any)
	if !array {
		candidates = []any{selected}
	}
	var matches []any
	for _, candidate := range candidates {
		match := true
		for selector, expected := range f.Match {
			actual, e := field(candidate, selector)
			if e != nil || actual != expected {
				match = false
				break
			}
		}
		if match {
			matches = append(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return Selection{}, fmt.Errorf("feed asset selection matched %d entries; expected one", len(matches))
	}
	asset := matches[0]
	download, err := field(asset, f.URL)
	if err != nil {
		return Selection{}, err
	}
	allowed := false
	for _, prefix := range f.AllowedDownloadPrefixes {
		if strings.HasPrefix(download, prefix) {
			allowed = true
			break
		}
	}
	if !httpsURL(download) || !allowed {
		return Selection{}, errors.New("unexpected feed download URL")
	}
	parsed, _ := url.Parse(download)
	name := path.Base(parsed.Path)
	if _, err = files.Relative(name); err != nil || strings.ContainsAny(name, "/\\") {
		return Selection{}, errors.New("unsafe feed package filename")
	}
	result := Selection{Tag: tag, Asset: Asset{Name: name, URL: download}}
	if f.UpdatedAt != "" {
		result.Asset.UpdatedAt, err = field(document, f.UpdatedAt)
		if err != nil {
			return Selection{}, err
		}
	}
	if f.SHA256 != "" {
		checksum, e := field(asset, f.SHA256)
		if e != nil {
			return Selection{}, e
		}
		checksum = strings.ToLower(checksum)
		if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(checksum) {
			return Selection{}, errors.New("invalid feed SHA-256")
		}
		result.Asset.Digest = "sha256:" + checksum
	} else if s.Checksum != "when-published" {
		return Selection{}, errors.New("feed requires a publisher SHA-256 field")
	}
	if f.SizeFromHead {
		req, e := http.NewRequestWithContext(ctx, "HEAD", download, nil)
		if e != nil {
			return Selection{}, e
		}
		response, e := httpx.Client.Do(req)
		if e != nil {
			return Selection{}, e
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return Selection{}, fmt.Errorf("package size lookup returned HTTP %d", response.StatusCode)
		}
		result.Asset.Size = response.ContentLength
	} else {
		raw, ok := pointer(asset, f.Size)
		if !ok {
			return Selection{}, errors.New("missing feed package size")
		}
		// Re-encode to reject strings, fractions and values beyond int64 without rounding.
		encoded, e := json.Marshal(raw)
		if e != nil {
			return Selection{}, e
		}
		if e = json.Unmarshal(encoded, &result.Asset.Size); e != nil {
			return Selection{}, errors.New("invalid feed package size")
		}
	}
	if result.Asset.Size <= 0 || result.Asset.Size > 2<<30 {
		return Selection{}, errors.New("feed package size is outside limits")
	}
	return result, nil
}
