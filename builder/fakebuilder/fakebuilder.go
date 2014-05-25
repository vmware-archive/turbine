package fakebuilder

import (
	"sync"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/builder"
)

type Builder struct {
	built        []builds.Build
	StartedBuild *builds.Build
	StartError   error

	attached      []builder.RunningBuild
	FinishedBuild builds.Build
	BuildFailure  error
	BuildError    error

	sync.RWMutex
}

func New() *Builder {
	return &Builder{}
}

func (fake *Builder) Build(build builds.Build) (<-chan builder.RunningBuild, <-chan error) {
	started := make(chan builder.RunningBuild)
	errored := make(chan error)

	fake.Lock()
	fake.built = append(fake.built, build)
	fake.Unlock()

	go func() {
		if fake.StartError != nil {
			errored <- fake.StartError
		} else if fake.StartedBuild != nil {
			started <- builder.RunningBuild{Build: *fake.StartedBuild}
		} else {
			started <- builder.RunningBuild{Build: build}
		}
	}()

	return started, errored
}

func (fake *Builder) Attach(runningBuild builder.RunningBuild) (<-chan builds.Build, <-chan error, <-chan error) {
	finished := make(chan builds.Build)
	failed := make(chan error)
	errored := make(chan error)

	fake.Lock()
	fake.attached = append(fake.attached, runningBuild)
	fake.Unlock()

	go func() {
		if fake.BuildError != nil {
			errored <- fake.BuildError
		} else if fake.BuildFailure != nil {
			failed <- fake.BuildFailure
		} else {
			finished <- runningBuild.Build
		}
	}()

	return finished, failed, errored
}

func (fake *Builder) Built() []builds.Build {
	fake.RLock()

	built := make([]builds.Build, len(fake.built))
	copy(built, fake.built)

	fake.RUnlock()

	return built
}

func (fake *Builder) Attached() []builder.RunningBuild {
	fake.RLock()

	attached := make([]builder.RunningBuild, len(fake.attached))
	copy(attached, fake.attached)

	fake.RUnlock()

	return attached
}
