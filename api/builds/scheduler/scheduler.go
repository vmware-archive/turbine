package scheduler

import "github.com/room101-ci/agent/api/builds"

type Scheduler struct {
}

func NewScheduler() *Scheduler {
	return &Scheduler{}
}

func (scheduler *Scheduler) Schedule(build *builds.Build) error {
	return nil
}
