package config

import "testing"

func TestSnapshotSelectionsRejectOverlaps(t *testing.T) {
	for _, paths := range [][]string{{"data", "data/saves"}, {"data/saves", "data"}, {"data", "data"}, {"util"}, {"tmp/portable-update/cache"}} {
		if err := ValidateSnapshotPaths(t.TempDir(), paths); err == nil {
			t.Fatalf("overlap accepted: %v", paths)
		}
	}
	if err := ValidateSnapshotPaths(t.TempDir(), []string{"data/saves", "data/settings"}); err != nil {
		t.Fatal(err)
	}
}
