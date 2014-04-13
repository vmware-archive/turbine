package builder

import "github.com/room101-ci/agent/api/builds"

type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

func (builder *Builder) Build(build *builds.Build) (bool, error) {
	return true, nil
}
