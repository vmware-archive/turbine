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
	"github.com/concourse/turbine/builder"
	sfakes "github.com/concourse/turbine/scheduler/fakes"
	. "github.com/concourse/turbine/snapshotter"
)

var _ = Describe("Snapshotter", func() {
	var snapshotPath string
	var scheduler *sfakes.FakeScheduler
	var snapshotter *Snapshotter

	var process ifrit.Process

	theSnapshots := []BuildSnapshot{
		{
			Build: builds.Build{
				Config: builds.Config{
					Run: builds.RunConfig{
						Path: "some-script",
					},
				},
			},
			ProcessID: 123,
		},
		{
			Build: builds.Build{
				Config: builds.Config{
					Run: builds.RunConfig{
						Path: "some-other-script",
					},
				},
			},
			ProcessID: 124,
		},
	}

	theRunningBuilds := []builder.RunningBuild{
		{
			Build: builds.Build{
				Config: builds.Config{
					Run: builds.RunConfig{
						Path: "some-script",
					},
				},
			},
			ProcessID: 123,
		},
		{
			Build: builds.Build{
				Config: builds.Config{
					Run: builds.RunConfig{
						Path: "some-other-script",
					},
				},
			},
			ProcessID: 124,
		},
	}

	BeforeEach(func() {
		snapshotFile, err := ioutil.TempFile("", "snapshot-file")
		Ω(err).ShouldNot(HaveOccurred())

		snapshotFile.Close()

		snapshotPath = snapshotFile.Name()

		scheduler = new(sfakes.FakeScheduler)
		snapshotter = NewSnapshotter(lagertest.NewTestLogger("test"), snapshotPath, scheduler)
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

			It("attaches to the builds via the scheduler", func() {
				Eventually(scheduler.AttachCallCount).Should(Equal(len(theRunningBuilds)))

				for i, build := range theRunningBuilds {
					Ω(scheduler.AttachArgsForCall(i)).Should(Equal(build))
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
