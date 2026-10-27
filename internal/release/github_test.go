package release

import (
	"crypto/sha256"
	"encoding/hex"
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
