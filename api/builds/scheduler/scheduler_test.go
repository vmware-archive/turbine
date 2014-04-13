package scheduler_test

import (
	. "github.com/onsi/ginkgo"

	. "github.com/room101-ci/agent/api/builds/scheduler"
)

var _ = Describe("Scheduler", func() {
	var scheduler *Scheduler

	BeforeEach(func() {
		scheduler = NewScheduler()
	})

	Describe("Schedule", func() {
		PIt("fetches the source", func() {
		})
	})
})
