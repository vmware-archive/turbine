package fakebuilder

import (
	"sync"

	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/builder"
)

type Builder struct {
	WhenStarting func(builds.Build, <-chan struct{}) (<-chan builder.RunningBuild, <-chan error)
	built        []builds.Build
	StartedBuild *builds.Build
	StartError   error

	WhenAttaching  func(builder.RunningBuild, <-chan struct{}) (<-chan builder.SucceededBuild, <-chan error, <-chan error)
	attached       []builder.RunningBuild
	SucceededBuild *builds.Build
	BuildFailure   error
	BuildError     error

	WhenCompleting func(builder.SucceededBuild, <-chan struct{}) (<-chan builds.Build, <-chan error)
	completed      []builder.SucceededBuild
	FinishedBuild  builds.Build
	CompleteError  error

	hijacked    []HijackSpec
	HijackError error

	sync.RWMutex
}

type HijackSpec struct {
	Build builder.RunningBuild
	Spec  warden.ProcessSpec
	IO    warden.ProcessIO
}

func New() *Builder {
	return &Builder{}
}

func (fake *Builder) Start(build builds.Build, abort <-chan struct{}) (<-chan builder.RunningBuild, <-chan error) {
	if fake.WhenStarting != nil {
		return fake.WhenStarting(build, abort)
	}

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

func (fake *Builder) Attach(runningBuild builder.RunningBuild, abort <-chan struct{}) (<-chan builder.SucceededBuild, <-chan error, <-chan error) {
	if fake.WhenAttaching != nil {
		return fake.WhenAttaching(runningBuild, abort)
	}

	succeeded := make(chan builder.SucceededBuild)
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
		} else if fake.SucceededBuild != nil {
			succeeded <- builder.SucceededBuild{Build: *fake.SucceededBuild}
		} else {
			succeeded <- builder.SucceededBuild{Build: runningBuild.Build}
		}
	}()

	return succeeded, failed, errored
}

func (fake *Builder) Complete(succeededBuild builder.SucceededBuild, abort <-chan struct{}) (<-chan builds.Build, <-chan error) {
	if fake.WhenCompleting != nil {
		return fake.WhenCompleting(succeededBuild, abort)
	}

	finished := make(chan builds.Build)
	errored := make(chan error)

	fake.Lock()
	fake.completed = append(fake.completed, succeededBuild)
	fake.Unlock()

	go func() {
		if fake.CompleteError != nil {
			errored <- fake.CompleteError
		} else {
			finished <- fake.FinishedBuild
		}
	}()

	return finished, errored
}

func (fake *Builder) Hijack(runningBuild builder.RunningBuild, spec warden.ProcessSpec, io warden.ProcessIO) error {
	fake.Lock()
	fake.hijacked = append(fake.hijacked, HijackSpec{
		Build: runningBuild,
		Spec:  spec,
		IO:    io,
	})
	fake.Unlock()

	return fake.HijackError
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

func (fake *Builder) Completed() []builder.SucceededBuild {
	fake.RLock()

	completed := make([]builder.SucceededBuild, len(fake.completed))
	copy(completed, fake.completed)

	fake.RUnlock()

	return completed
}

func (fake *Builder) Hijacked() []HijackSpec {
	fake.RLock()

	hijacked := make([]HijackSpec, len(fake.hijacked))
	copy(hijacked, fake.hijacked)

	fake.RUnlock()

	return hijacked
}
