package fakescheduler

import (
	"sync"

	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/builder"
)

type FakeScheduler struct {
	scheduled []builds.Build
	attached  []builder.RunningBuild
	aborted   []string

	DrainResult []builder.RunningBuild

	sync.RWMutex
}

func New() *FakeScheduler {
	return &FakeScheduler{}
}

func (scheduler *FakeScheduler) Start(build builds.Build) {
	scheduler.Lock()
	scheduler.scheduled = append(scheduler.scheduled, build)
	scheduler.Unlock()
}

func (scheduler *FakeScheduler) Attach(running builder.RunningBuild) {
	scheduler.Lock()
	scheduler.attached = append(scheduler.attached, running)
	scheduler.Unlock()
}

func (scheduler *FakeScheduler) Drain() []builder.RunningBuild {
	return scheduler.DrainResult
}

func (scheduler *FakeScheduler) Abort(guid string) {
	scheduler.Lock()
	scheduler.aborted = append(scheduler.aborted, guid)
	scheduler.Unlock()
}

func (scheduler *FakeScheduler) Scheduled() []builds.Build {
	scheduler.RLock()

	scheduled := make([]builds.Build, len(scheduler.scheduled))
	copy(scheduled, scheduler.scheduled)

	scheduler.RUnlock()

	return scheduled
}

func (scheduler *FakeScheduler) Attached() []builder.RunningBuild {
	scheduler.RLock()

	attached := make([]builder.RunningBuild, len(scheduler.attached))
	copy(attached, scheduler.attached)

	scheduler.RUnlock()

	return attached
}

func (scheduler *FakeScheduler) Aborted() []string {
	scheduler.RLock()

	aborted := make([]string, len(scheduler.aborted))
	copy(aborted, scheduler.aborted)

	scheduler.RUnlock()

	return aborted
}
