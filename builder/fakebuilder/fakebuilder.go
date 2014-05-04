package fakebuilder

import (
	"sync"

	"github.com/winston-ci/prole/api/builds"
)

type Builder struct {
	built       []builds.Build
	BuildResult bool
	BuildError  error

	sync.RWMutex
}

func New() *Builder {
	return &Builder{}
}

func (builder *Builder) Build(build builds.Build) (bool, error) {
	if builder.BuildError != nil {
		return false, builder.BuildError
	}

	builder.Lock()
	builder.built = append(builder.built, build)
	builder.Unlock()

	return builder.BuildResult, nil
}

func (builder *Builder) Built() []builds.Build {
	builder.RLock()

	built := make([]builds.Build, len(builder.built))
	copy(built, builder.built)

	builder.RUnlock()

	return built
}
