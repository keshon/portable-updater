package release

import (
	"context"
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
