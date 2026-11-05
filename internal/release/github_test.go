package release

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestSourceSelection(t *testing.T) {
	source := Source{Repository: "PCSX2/pcsx2", AssetPattern: `^pcsx2-.*-windows-x64-Qt\.7z$`}
	asset := testAsset([]byte("archive"), "https://github.com/PCSX2/pcsx2/releases/download/v2.9.1/pcsx2-v2.9.1-windows-x64-Qt.7z")
	asset.Name = "pcsx2-v2.9.1-windows-x64-Qt.7z"
	releases := []Release{{Tag: "v2.9.1", Prerelease: true, Published: time.Now(), Assets: []Asset{asset}}}
	if _, e := selectRelease(releases, source, "nightly"); e != nil {
		t.Fatal(e)
	}
	if _, e := selectRelease(releases, source, "stable"); e == nil {
		t.Fatal("nightly selected for stable")
	}
	releases[0].Assets[0].Digest = ""
	if _, e := selectRelease(releases, source, "nightly"); e == nil {
		t.Fatal("missing digest accepted")
	}
}
func testAsset(data []byte, url string) Asset {
	h := sha256.Sum256(data)
	return Asset{Name: "package.7z", URL: url, Digest: "sha256:" + hex.EncodeToString(h[:]), Size: int64(len(data))}
}

func TestCompanionSelectionIsCompleteAndVerified(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "ambiguous", "digest", "url", "overlap"} {
		t.Run(mode, func(t *testing.T) {
			source := Source{Provider: "github", Repository: "owner/project", AssetPattern: `^app.zip$`, CompanionPatterns: []string{`^runtime.*\.zip$`}}
			asset := testAsset([]byte("app"), "https://github.com/owner/project/releases/download/v1/app.zip")
			asset.Name = "app.zip"
			companion := testAsset([]byte("runtime"), "https://github.com/owner/project/releases/download/v1/runtime.zip")
			companion.Name = "runtime.zip"
			assets := []Asset{asset, companion}
			switch mode {
			case "missing":
				assets = assets[:1]
			case "ambiguous":
				other := companion
				other.Name = "runtime-alt.zip"
				assets = append(assets, other)
			case "digest":
				assets[1].Digest = ""
			case "url":
				assets[1].URL = "https://example.invalid/runtime.zip"
			case "overlap":
				source.CompanionPatterns = []string{`^app.zip$`}
			}
			selected, err := selectRelease([]Release{{Tag: "v1", Assets: assets}}, source, "latest")
			if mode == "valid" {
				if err != nil || len(selected.Companions) != 1 {
					t.Fatalf("%+v, %v", selected, err)
				}
			} else if err == nil {
				t.Fatalf("%s accepted", mode)
			}
		})
	}
}

func TestCompanionIdentityAffectsSelection(t *testing.T) {
	// Each pattern is independent; an architecture lookalike is not a match.
	source := Source{Provider: "github", Repository: "owner/project", AssetPattern: `^app-x64.zip$`, CompanionPatterns: []string{`^deps-x64.zip$`}}
	a := testAsset([]byte("app"), "https://github.com/owner/project/releases/download/v1/app-x64.zip")
	a.Name = "app-x64.zip"
	b := testAsset([]byte("deps"), "https://github.com/owner/project/releases/download/v1/deps-arm64.zip")
	b.Name = "deps-arm64.zip"
	_, err := selectRelease([]Release{{Tag: "v1", Assets: []Asset{a, b}}}, source, "latest")
	if err == nil || !strings.Contains(err.Error(), "companion") {
		t.Fatal(err)
	}
}
