package fakebuilder

import (
	"sync"

	"github.com/winston-ci/prole/api/builds"
)

type Builder struct {
	built        []builds.Build
	StartError   error
	StartedBuild *builds.Build
	BuildResult  bool
	BuildError   error

	sync.RWMutex
}

func New() *Builder {
	return &Builder{}
}

func (builder *Builder) Build(build builds.Build) (<-chan builds.Build, <-chan bool, <-chan error) {
	started := make(chan builds.Build)
	finished := make(chan bool)
	errored := make(chan error)

	builder.Lock()
	builder.built = append(builder.built, build)
	builder.Unlock()

	go func() {
		if builder.StartError != nil {
			errored <- builder.StartError
		} else {
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
		}
	}()

	return started, finished, errored
}

func (builder *Builder) Built() []builds.Build {
	builder.RLock()

	built := make([]builds.Build, len(builder.built))
	copy(built, builder.built)

	builder.RUnlock()

	return built
}
