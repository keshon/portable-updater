package updater

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/keshon/portable-updater/internal/platform"
)

type Version [4]uint64

func parseVersion(value string) (Version, error) {
	var result Version
	value = strings.TrimPrefix(value, "v")
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+){0,2}$`).MatchString(value) {
		return result, fmt.Errorf("unsupported version format: %q", value)
	}
	for i, part := range strings.Split(value, ".") {
		n, e := strconv.ParseUint(part, 10, 32)
		if e != nil {
			return result, e
		}
		result[i] = n
	}
	return result, nil
}
func compareVersion(a, b Version) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

var executableVersion = platform.FileVersion
