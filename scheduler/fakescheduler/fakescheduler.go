package fakescheduler

import (
	"sync"

	"github.com/room101-ci/agent/api/builds"
)

type FakeScheduler struct {
	scheduled     []*builds.Build
	ScheduleError error

	sync.RWMutex
}

func New() *FakeScheduler {
	return &FakeScheduler{}
}

func (scheduler *FakeScheduler) Schedule(build *builds.Build) error {
	if scheduler.ScheduleError != nil {
		return scheduler.ScheduleError
	}

	scheduler.Lock()

	scheduler.scheduled = append(scheduler.scheduled, build)

	scheduler.Unlock()

	return nil
}

func (scheduler *FakeScheduler) Scheduled() []*builds.Build {
	scheduler.RLock()

	scheduled := make([]*builds.Build, len(scheduler.scheduled))
	copy(scheduled, scheduler.scheduled)

	scheduler.RUnlock()

	return scheduled
}
