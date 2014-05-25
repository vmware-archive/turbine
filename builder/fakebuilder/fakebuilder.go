package fakebuilder

import (
	"sync"

	"github.com/winston-ci/prole/api/builds"
)

type Builder struct {
	built        []builds.Build
	StartError   error
	StartedBuild *builds.Build
	BuildFailure error
	BuildResult  builds.Build
	BuildError   error

	sync.RWMutex
}

func New() *Builder {
	return &Builder{}
}

func (builder *Builder) Build(build builds.Build) (<-chan builds.Build, <-chan error, <-chan error, <-chan builds.Build) {
	started := make(chan builds.Build)
	failed := make(chan error)
	errored := make(chan error)
	finished := make(chan builds.Build)

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
			} else if builder.BuildFailure != nil {
				failed <- builder.BuildFailure
			} else {
				finished <- builder.BuildResult
			}
		}
	}()

	return started, failed, errored, finished
}

func (builder *Builder) Built() []builds.Build {
	builder.RLock()

	built := make([]builds.Build, len(builder.built))
	copy(built, builder.built)

	builder.RUnlock()

	return built
}
