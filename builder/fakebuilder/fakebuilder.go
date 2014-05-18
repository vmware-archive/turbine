package fakebuilder

import (
	"sync"

	"github.com/winston-ci/prole/api/builds"
)

type Builder struct {
	built        []builds.Build
	StartedBuild *builds.Build
	BuildResult  bool
	BuildError   error

	sync.RWMutex
}

func New() *Builder {
	return &Builder{}
}

func (builder *Builder) Build(build builds.Build) (<-chan builds.Build, <-chan bool, <-chan error) {
	started := make(chan builds.Build, 1)
	finished := make(chan bool, 1)
	errored := make(chan error, 1)

	builder.Lock()
	builder.built = append(builder.built, build)
	builder.Unlock()

	if builder.StartedBuild != nil {
		started <- *builder.StartedBuild
	} else {
		started <- build
	}

	if builder.BuildError != nil {
		errored <- builder.BuildError
	} else {
		finished <- builder.BuildResult
	}

	return started, finished, errored
}

func (builder *Builder) Built() []builds.Build {
	builder.RLock()

	built := make([]builds.Build, len(builder.built))
	copy(built, builder.built)

	builder.RUnlock()

	return built
}
