package updater

import (
	"testing"
)

func TestVersionComparison(t *testing.T) {
	a, _ := parseVersion("v2.9.146")
	b, _ := parseVersion("2.9.146.0")
	if compareVersion(a, b) != 0 {
		t.Fatal("trailing zero version differs")
	}
	older, _ := parseVersion("2.5.393.0")
	if compareVersion(older, a) != -1 {
		t.Fatal("upgrade order incorrect")
	}
	if _, e := parseVersion("release-nightly"); e == nil {
		t.Fatal("unknown version accepted")
	}
}
