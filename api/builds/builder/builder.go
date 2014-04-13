package builder

import "github.com/room101-ci/agent/api/builds"

type Fetcher interface {
	Fetch(source builds.BuildSource) (directory string, err error)
}

type Builder struct {
	fetcher Fetcher
}

func NewBuilder(fetcher Fetcher) *Builder {
	return &Builder{
		fetcher: fetcher,
	}
}

func (builder *Builder) Build(build *builds.Build) (bool, error) {
	_, err := builder.fetcher.Fetch(build.Source)
	if err != nil {
		return false, err
	}

	return true, nil
}
