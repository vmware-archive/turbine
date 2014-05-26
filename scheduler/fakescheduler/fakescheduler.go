package fakescheduler

import (
	"sync"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/builder"
)

type FakeScheduler struct {
	scheduled []builds.Build
	attached  []builder.RunningBuild

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
