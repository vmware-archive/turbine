package snapshotter_test

import (
	"encoding/json"
	"io/ioutil"
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/ifrit"

	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/event"
	sched "github.com/concourse/turbine/scheduler"
	sfakes "github.com/concourse/turbine/scheduler/fakes"
	. "github.com/concourse/turbine/snapshotter"
)

var _ = Describe("Snapshotter", func() {
	var snapshotPath string
	var scheduler *sfakes.FakeScheduler
	var snapshotter *Snapshotter

	var process ifrit.Process

	var theSnapshots []BuildSnapshot
	var theRunningBuilds []sched.ScheduledBuild

	BeforeEach(func() {
		snapshotFile, err := ioutil.TempFile("", "snapshot-file")
		Ω(err).ShouldNot(HaveOccurred())

		snapshotFile.Close()

		snapshotPath = snapshotFile.Name()

		scheduler = new(sfakes.FakeScheduler)
		snapshotter = NewSnapshotter(lagertest.NewTestLogger("test"), snapshotPath, scheduler)

		firstHub := event.NewHub()
		firstHub.EmitEvent(event.Version("0.0"))
		firstHub.EmitEvent(event.Start{Time: 1})

		secondHub := event.NewHub()
		secondHub.EmitEvent(event.Version("1.0"))
		secondHub.EmitEvent(event.Start{Time: 2})

		theRunningBuilds = []sched.ScheduledBuild{
			{
				Build: builds.Build{
					Config: builds.Config{
						Run: builds.RunConfig{
							Path: "some-script",
						},
					},
				},
				Status:    builds.StatusStarted,
				ProcessID: 123,
				EventHub:  firstHub,
			},
			{
				Build: builds.Build{
					Config: builds.Config{
						Run: builds.RunConfig{
							Path: "some-other-script",
						},
					},
				},
				Status:    builds.StatusSucceeded,
				ProcessID: 124,
				EventHub:  secondHub,
			},
		}

		theSnapshots = []BuildSnapshot{
			{
				Build: builds.Build{
					Config: builds.Config{
						Run: builds.RunConfig{
							Path: "some-script",
						},
					},
				},
				Status:    builds.StatusStarted,
				ProcessID: 123,
				Events: []event.Message{
					{event.Version("0.0")},
					{event.Start{Time: 1}},
				},
			},
			{
				Build: builds.Build{
					Config: builds.Config{
						Run: builds.RunConfig{
							Path: "some-other-script",
						},
					},
				},
				Status:    builds.StatusSucceeded,
				ProcessID: 124,
				Events: []event.Message{
					{event.Version("1.0")},
					{event.Start{Time: 2}},
				},
			},
		}
	})

	AfterEach(func() {
		os.RemoveAll(snapshotPath)
	})

	JustBeforeEach(func() {
		process = ifrit.Envoke(snapshotter)
	})

	Context("when the snapshot does not exist", func() {
		BeforeEach(func() {
			os.RemoveAll(snapshotPath)
		})

		It("does not fail", func() {
			Consistently(process.Wait()).ShouldNot(Receive())
		})

		Describe("when a signal is received", func() {
			JustBeforeEach(func() {
				process.Signal(os.Interrupt)
			})

			BeforeEach(func() {
				scheduler.DrainReturns(theRunningBuilds)
			})

			It("drains the scheduler and snapshots the results", func() {
				Eventually(process.Wait()).Should(Receive(BeNil()))

				snapshotFile, err := os.Open(snapshotPath)
				Ω(err).ShouldNot(HaveOccurred())

				var snapshots []BuildSnapshot
				err = json.NewDecoder(snapshotFile).Decode(&snapshots)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(snapshots).Should(Equal(theSnapshots))
			})
		})
	})

	Context("when a snapshot exists", func() {
		Context("and it contains valid JSON", func() {
			BeforeEach(func() {
				snapshot, err := json.Marshal(theSnapshots)
				Ω(err).ShouldNot(HaveOccurred())

				err = ioutil.WriteFile(snapshotPath, snapshot, 0644)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("restores the builds to the scheduler", func() {
				Eventually(scheduler.RestoreCallCount).Should(Equal(len(theRunningBuilds)))

				for i, build := range theRunningBuilds {
					restored := scheduler.RestoreArgsForCall(i)
					Ω(restored.Build).Should(Equal(build.Build))
					Ω(restored.Status).Should(Equal(build.Status))
					Ω(restored.ProcessID).Should(Equal(build.ProcessID))
					Ω(restored.EventHub.Events()).Should(Equal(build.EventHub.Events()))
				}
			})
		})

		Context("and it contains invalid JSON", func() {
			BeforeEach(func() {
				err := ioutil.WriteFile(snapshotPath, []byte("ß"), 0644)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("does not exit", func() {
				Consistently(process.Wait()).ShouldNot(Receive())
			})
		})
	})
})
