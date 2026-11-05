package portableupdater

import "embed"

// Files contains the canonical distribution assets used by first-time setup.
//
//go:embed adapters/*.json examples/*.json schemas/*.json scripts/launchers/*.cmd LICENSE THIRD-PARTY-LICENSES.txt
var Files embed.FS
