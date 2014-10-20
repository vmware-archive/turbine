package scheduler_test

import (
	"bytes"
	"errors"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager/lagertest"

	garden_api "github.com/cloudfoundry-incubator/garden/api"
	gfakes "github.com/cloudfoundry-incubator/garden/api/fakes"
	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/builder"
	bfakes "github.com/concourse/turbine/builder/fakes"
	"github.com/concourse/turbine/event"
	. "github.com/concourse/turbine/scheduler"
	"github.com/concourse/turbine/scheduler/fakes"
)

var _ = Describe("Scheduler", func() {
	var (
		fakeBuilder *bfakes.FakeBuilder
		clock       *fakes.FakeClock

		scheduler Scheduler

		build builds.Build
	)

	BeforeEach(func() {
		fakeBuilder = new(bfakes.FakeBuilder)
		clock = new(fakes.FakeClock)

		logger := lagertest.NewTestLogger("test")
		scheduler = NewScheduler(logger, fakeBuilder, clock)

		build = builds.Build{
			Guid: "abc",

			Inputs: []builds.Input{
				{
					Type: "git",
				},
			},

			Config: builds.Config{
				Params: map[string]string{
					"FOO":  "bar",
					"FIZZ": "buzz",
				},
			},
		}

		// default to happy path
		fakeBuilder.StartReturns(builder.RunningBuild{
			Build:     build,
			ProcessID: 42,
		}, nil)

		fakeBuilder.AttachReturns(builder.ExitedBuild{
			Build:      build,
			ExitStatus: 0,
		}, nil)

		fakeBuilder.FinishReturns(build, nil)
	})

	subscribeToBuildEvents := func() (<-chan event.Event, chan<- struct{}) {
		events, stop, err := scheduler.Subscribe(build.Guid, 0)
		Ω(err).ShouldNot(HaveOccurred())

		return events, stop
	}

	Describe("Start", func() {
		It("kicks off a builder", func() {
			scheduler.Start(build)

			Eventually(fakeBuilder.StartCallCount).Should(Equal(1))

			startedBuild, _, _ := fakeBuilder.StartArgsForCall(0)
			Ω(startedBuild).Should(Equal(build))
		})

		It("emits the current event version", func() {
			scheduler.Start(build)

			emittedEvents, stop := subscribeToBuildEvents()
			defer close(stop)

			Eventually(emittedEvents).Should(Receive(Equal(event.CURRENT_VERSION)))
		})

		Context("and the build starts", func() {
			var running builder.RunningBuild

			var (
				startTime time.Time
				endTime   time.Time
			)

			BeforeEach(func() {
				startTime = time.Now()
				endTime = startTime.Add(10 * time.Second)

				clock.CurrentTimeReturns(startTime)

				running = builder.RunningBuild{
					Build: build,
				}

				fakeBuilder.StartReturns(running, nil)
			})

			It("emits a started status event", func() {
				scheduler.Start(build)

				emittedEvents, stop := subscribeToBuildEvents()
				defer close(stop)

				Eventually(emittedEvents).Should(Receive(Equal(event.Status{
					Status: builds.StatusStarted,
					Time:   startTime.Unix(),
				})))
			})

			Context("when the build exits 0", func() {
				var exited builder.ExitedBuild

				BeforeEach(func() {
					exited = builder.ExitedBuild{
						Build: running.Build,

						ExitStatus: 0,
					}

					fakeBuilder.AttachReturns(exited, nil)
				})

				It("finishes the build", func() {
					scheduler.Start(build)

					Eventually(fakeBuilder.FinishCallCount).Should(Equal(1))

					completing, _, _ := fakeBuilder.FinishArgsForCall(0)
					Ω(completing).Should(Equal(exited))
				})

				Context("and the build finishes", func() {
					BeforeEach(func() {
						fakeBuilder.FinishStub = func(builder.ExitedBuild, event.Emitter, <-chan struct{}) (builds.Build, error) {
							clock.CurrentTimeReturns(endTime)
							return exited.Build, nil
						}
					})

					It("emits a succeeded status event, ends the stream, and closes it", func() {
						scheduler.Start(build)

						emittedEvents, stop := subscribeToBuildEvents()
						defer close(stop)

						Eventually(emittedEvents).Should(Receive(Equal(event.Status{
							Status: builds.StatusSucceeded,
							Time:   endTime.Unix(),
						})))

						Eventually(emittedEvents).Should(Receive(Equal(event.End{})))
						Eventually(emittedEvents).Should(BeClosed())
					})
				})

				Context("and the build fails to finish", func() {
					BeforeEach(func() {
						fakeBuilder.FinishStub = func(builder.ExitedBuild, event.Emitter, <-chan struct{}) (builds.Build, error) {
							clock.CurrentTimeReturns(endTime)
							return builds.Build{}, errors.New("oh no!")
						}
					})

					It("emits an errored status event, ends the stream, and closes it", func() {
						scheduler.Start(build)

						emittedEvents, stop := subscribeToBuildEvents()
						defer close(stop)

						Eventually(emittedEvents).Should(Receive(Equal(event.Status{
							Status: builds.StatusErrored,
							Time:   endTime.Unix(),
						})))

						Eventually(emittedEvents).Should(Receive(Equal(event.End{})))
						Eventually(emittedEvents).Should(BeClosed())
					})
				})
			})

			Context("when the build exited nonzero", func() {
				var exited builder.ExitedBuild

				BeforeEach(func() {
					exited = builder.ExitedBuild{
						Build: running.Build,

						ExitStatus: 2,
					}

					fakeBuilder.AttachReturns(exited, nil)
				})

				It("finishes the build", func() {
					scheduler.Start(build)

					Eventually(fakeBuilder.FinishCallCount).Should(Equal(1))

					completing, _, _ := fakeBuilder.FinishArgsForCall(0)
					Ω(completing).Should(Equal(exited))
				})

				Context("and the build finishes", func() {
					BeforeEach(func() {
						fakeBuilder.FinishStub = func(builder.ExitedBuild, event.Emitter, <-chan struct{}) (builds.Build, error) {
							clock.CurrentTimeReturns(endTime)
							return exited.Build, nil
						}
					})

					It("emits a failed status event, ends the stream, and closes it", func() {
						scheduler.Start(build)

						emittedEvents, stop := subscribeToBuildEvents()
						defer close(stop)

						Eventually(emittedEvents).Should(Receive(Equal(event.Status{
							Status: builds.StatusFailed,
							Time:   endTime.Unix(),
						})))

						Eventually(emittedEvents).Should(Receive(Equal(event.End{})))
						Eventually(emittedEvents).Should(BeClosed())
					})
				})

				Context("and the build fails to finish", func() {
					BeforeEach(func() {
						fakeBuilder.FinishStub = func(builder.ExitedBuild, event.Emitter, <-chan struct{}) (builds.Build, error) {
							clock.CurrentTimeReturns(endTime)
							return exited.Build, errors.New("oh no!")
						}
					})

					It("emits an errored status event, ends the stream, and closes it", func() {
						scheduler.Start(build)

						emittedEvents, stop := subscribeToBuildEvents()
						defer close(stop)

						Eventually(emittedEvents).Should(Receive(Equal(event.Status{
							Status: builds.StatusErrored,
							Time:   endTime.Unix(),
						})))

						Eventually(emittedEvents).Should(Receive(Equal(event.End{})))
						Eventually(emittedEvents).Should(BeClosed())
					})
				})
			})

			Context("when building fails", func() {
				BeforeEach(func() {
					fakeBuilder.AttachStub = func(builder.RunningBuild, event.Emitter, <-chan struct{}) (builder.ExitedBuild, error) {
						clock.CurrentTimeReturns(endTime)
						return builder.ExitedBuild{}, errors.New("oh no!")
					}
				})

				It("emits an errored status event, ends the stream, and closes it", func() {
					scheduler.Start(build)

					emittedEvents, stop := subscribeToBuildEvents()
					defer close(stop)

					Eventually(emittedEvents).Should(Receive(Equal(event.Status{
						Status: builds.StatusErrored,
						Time:   endTime.Unix(),
					})))

					Eventually(emittedEvents).Should(Receive(Equal(event.End{})))
					Eventually(emittedEvents).Should(BeClosed())
				})
			})
		})

		Context("and the build fails to start", func() {
			var startTime time.Time
			var endTime time.Time

			BeforeEach(func() {
				startTime = time.Now()
				endTime = startTime

				clock.CurrentTimeReturns(startTime)

				fakeBuilder.StartStub = func(builds.Build, event.Emitter, <-chan struct{}) (builder.RunningBuild, error) {
					clock.CurrentTimeReturns(endTime)
					return builder.RunningBuild{}, errors.New("oh no!")
				}
			})

			It("emits an errored status event, ends the stream, and closes it", func() {
				scheduler.Start(build)

				emittedEvents, stop := subscribeToBuildEvents()
				defer close(stop)

				Eventually(emittedEvents).Should(Receive(Equal(event.Status{
					Status: builds.StatusErrored,
					Time:   endTime.Unix(),
				})))

				Eventually(emittedEvents).Should(Receive(Equal(event.End{})))
				Eventually(emittedEvents).Should(BeClosed())
			})
		})
	})

	Describe("Restore", func() {
		var scheduledBuild ScheduledBuild

		JustBeforeEach(func() {
			scheduler.Restore(scheduledBuild)
		})

		Context("with a completed build", func() {
			BeforeEach(func() {
				hub := event.NewHub()
				hub.EmitEvent(event.Version("1.0"))
				hub.EmitEvent(event.Start{Time: 1})

				scheduledBuild = ScheduledBuild{
					Build:     build,
					Status:    builds.StatusSucceeded,
					ProcessID: 2,
					EventHub:  hub,
				}
			})

			It("restores its event log", func() {
				events, stop, err := scheduler.Subscribe(build.Guid, 0)
				Ω(err).ShouldNot(HaveOccurred())

				defer close(stop)

				Eventually(events).Should(Receive(Equal(event.Version("1.0"))))
				Eventually(events).Should(Receive(Equal(event.Start{Time: 1})))
			})

			It("closes its event log without emitting a new version", func() {
				events, stop, err := scheduler.Subscribe(build.Guid, 0)
				Ω(err).ShouldNot(HaveOccurred())

				defer close(stop)

				Eventually(events).Should(Receive(Equal(event.Version("1.0"))))
				Eventually(events).Should(Receive(Equal(event.Start{Time: 1})))

				_, ok := <-events
				Ω(ok).Should(BeFalse())
			})

			It("does not try to attach to the build", func() {
				Consistently(fakeBuilder.AttachCallCount).Should(BeZero())
			})
		})

		Context("with a started build", func() {
			BeforeEach(func() {
				hub := event.NewHub()
				hub.EmitEvent(event.Version("1.0"))
				hub.EmitEvent(event.Start{Time: 1})

				scheduledBuild = ScheduledBuild{
					Build:     build,
					Status:    builds.StatusStarted,
					ProcessID: 2,
					EventHub:  hub,
				}
			})

			It("emits the current event version", func() {
				events, stop, err := scheduler.Subscribe(build.Guid, 0)
				Ω(err).ShouldNot(HaveOccurred())

				defer close(stop)

				Eventually(events).Should(Receive(Equal(event.Version("1.0"))))
				Eventually(events).Should(Receive(Equal(event.Start{Time: 1})))
				Eventually(events).Should(Receive(Equal(event.CURRENT_VERSION)))
			})

			It("re-attaches to the build via the builder", func() {
				Eventually(fakeBuilder.AttachCallCount).Should(Equal(1))

				runningBuild, hub, abort := fakeBuilder.AttachArgsForCall(0)

				Ω(runningBuild).Should(Equal(builder.RunningBuild{
					Build:     build,
					ProcessID: 2,
				}))

				Ω(hub).Should(Equal(scheduledBuild.EventHub))

				Ω(abort).ShouldNot(BeNil())
			})

			Context("while attached", func() {
				var exitedBuild chan builder.ExitedBuild

				BeforeEach(func() {
					exitedBuild = make(chan builder.ExitedBuild)

					fakeBuilder.AttachStub = func(builder.RunningBuild, event.Emitter, <-chan struct{}) (builder.ExitedBuild, error) {
						return <-exitedBuild, nil
					}
				})

				It("does not close the event stream", func() {
					events, stop, err := scheduler.Subscribe(build.Guid, 0)
					Ω(err).ShouldNot(HaveOccurred())

					defer close(stop)

					Consistently(events).ShouldNot(BeClosed())

					exitedTime := time.Now()
					clock.CurrentTimeReturns(exitedTime)

					exitedBuild <- builder.ExitedBuild{
						Build:      build,
						ExitStatus: 0,
					}

					Eventually(events).Should(Receive(Equal(event.Status{
						Status: builds.StatusSucceeded,
						Time:   exitedTime.Unix(),
					})))
				})
			})
		})
	})

	Describe("Hijack", func() {
		Context("when the builder fails to hijack", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				fakeBuilder.HijackReturns(nil, disaster)
			})

			It("returns an error", func() {
				_, err := scheduler.Hijack("some-guid", garden_api.ProcessSpec{}, garden_api.ProcessIO{})
				Ω(err).Should(Equal(disaster))
			})
		})

		Context("when the builder successfully hijacks", func() {
			BeforeEach(func() {
				fakeProcess := new(gfakes.FakeProcess)
				fakeProcess.WaitReturns(42, nil)

				fakeBuilder.HijackReturns(fakeProcess, nil)
			})

			It("hijacks via the builder", func() {
				spec := garden_api.ProcessSpec{
					Path: "process-path",
					Args: []string{"process", "args"},
					TTY: &garden_api.TTYSpec{
						WindowSize: &garden_api.WindowSize{
							Columns: 123,
							Rows:    456,
						},
					},
				}

				io := garden_api.ProcessIO{
					Stdin:  new(bytes.Buffer),
					Stdout: new(bytes.Buffer),
				}

				process, err := scheduler.Hijack("some-guid", spec, io)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(fakeBuilder.HijackCallCount()).Should(Equal(1))

				guid, spec, io := fakeBuilder.HijackArgsForCall(0)
				Ω(guid).Should(Equal("some-guid"))
				Ω(spec).Should(Equal(spec))
				Ω(io).Should(Equal(io))

				Ω(process.Wait()).Should(Equal(42))
			})
		})
	})

	Describe("Abort", func() {
		var currentTime time.Time

		BeforeEach(func() {
			currentTime = time.Now()
			clock.CurrentTimeReturns(currentTime)
		})

		Context("when starting a build", func() {
			var gotAborting chan (<-chan struct{})

			BeforeEach(func() {
				gotAborting = make(chan (<-chan struct{}), 1)

				fakeBuilder.StartStub = func(build builds.Build, emitter event.Emitter, abort <-chan struct{}) (builder.RunningBuild, error) {
					gotAborting <- abort
					<-abort
					return builder.RunningBuild{}, errors.New("aborted")
				}
			})

			It("signals to the builder to abort", func() {
				scheduler.Start(build)

				var abort <-chan struct{}
				Eventually(gotAborting).Should(Receive(&abort))

				scheduler.Abort(build.Guid)

				Ω(abort).Should(BeClosed())
			})

			It("emits an aborted status event, ends the stream, and closes it", func() {
				scheduler.Start(build)

				Eventually(gotAborting).Should(Receive())

				scheduler.Abort(build.Guid)

				emittedEvents, stop := subscribeToBuildEvents()
				defer close(stop)

				Eventually(emittedEvents).Should(Receive(Equal(event.Status{
					Status: builds.StatusAborted,
					Time:   currentTime.Unix(),
				})))

				Eventually(emittedEvents).Should(Receive(Equal(event.End{})))
				Eventually(emittedEvents).Should(BeClosed())
			})
		})

		Context("when attached to a build", func() {
			var gotAborting chan (<-chan struct{})

			BeforeEach(func() {
				gotAborting = make(chan (<-chan struct{}), 1)

				fakeBuilder.StartStub = func(build builds.Build, emitter event.Emitter, abort <-chan struct{}) (builder.RunningBuild, error) {
					return builder.RunningBuild{Build: build}, nil
				}

				fakeBuilder.AttachStub = func(build builder.RunningBuild, emitter event.Emitter, abort <-chan struct{}) (builder.ExitedBuild, error) {
					gotAborting <- abort
					<-abort
					return builder.ExitedBuild{}, errors.New("aborted")
				}
			})

			It("signals to the builder to abort", func() {
				scheduler.Start(build)

				var abort <-chan struct{}
				Eventually(gotAborting).Should(Receive(&abort))

				scheduler.Abort(build.Guid)

				Ω(abort).Should(BeClosed())
			})

			It("emits an aborted status event, ends the stream, and closes it", func() {
				scheduler.Start(build)

				Eventually(gotAborting).Should(Receive())

				scheduler.Abort(build.Guid)

				emittedEvents, stop := subscribeToBuildEvents()
				defer close(stop)

				Eventually(emittedEvents).Should(Receive(Equal(event.Status{
					Status: builds.StatusAborted,
					Time:   currentTime.Unix(),
				})))

				Eventually(emittedEvents).Should(Receive(Equal(event.End{})))
				Eventually(emittedEvents).Should(BeClosed())
			})
		})

		Context("when completing a build", func() {
			var gotAborting chan (<-chan struct{})

			BeforeEach(func() {
				gotAborting = make(chan (<-chan struct{}), 1)

				fakeBuilder.StartStub = func(build builds.Build, emitter event.Emitter, abort <-chan struct{}) (builder.RunningBuild, error) {
					return builder.RunningBuild{Build: build}, nil
				}

				fakeBuilder.AttachStub = func(running builder.RunningBuild, emitter event.Emitter, abort <-chan struct{}) (builder.ExitedBuild, error) {
					return builder.ExitedBuild{Build: running.Build}, nil
				}

				fakeBuilder.FinishStub = func(build builder.ExitedBuild, emitter event.Emitter, abort <-chan struct{}) (builds.Build, error) {
					gotAborting <- abort
					<-abort
					return builds.Build{}, errors.New("aborted")
				}
			})

			It("signals to the builder to abort", func() {
				scheduler.Start(build)

				var abort <-chan struct{}
				Eventually(gotAborting).Should(Receive(&abort))

				scheduler.Abort(build.Guid)

				Ω(abort).Should(BeClosed())
			})

			It("emits an aborted status event, ends the stream, and closes it", func() {
				scheduler.Start(build)

				Eventually(gotAborting).Should(Receive())

				scheduler.Abort(build.Guid)

				emittedEvents, stop := subscribeToBuildEvents()
				defer close(stop)

				Eventually(emittedEvents).Should(Receive(Equal(event.Status{
					Status: builds.StatusAborted,
					Time:   currentTime.Unix(),
				})))

				Eventually(emittedEvents).Should(Receive(Equal(event.End{})))
				Eventually(emittedEvents).Should(BeClosed())
			})
		})
	})

	Describe("Delete", func() {
		It("clears out the build's events", func() {
			scheduler.Start(build)

			_, stop, err := scheduler.Subscribe(build.Guid, 0)
			Ω(err).ShouldNot(HaveOccurred())

			defer close(stop)

			scheduler.Delete(build.Guid)

			_, _, err = scheduler.Subscribe(build.Guid, 0)
			Ω(err).Should(HaveOccurred())
		})
	})

	Describe("Drain", func() {
		var startTime time.Time

		BeforeEach(func() {
			startTime = time.Now()
			clock.CurrentTimeReturns(startTime)
		})

		Context("when a build is starting", func() {
			var running chan builder.RunningBuild

			BeforeEach(func() {
				running = make(chan builder.RunningBuild)

				fakeBuilder.StartStub = func(build builds.Build, emitter event.Emitter, abort <-chan struct{}) (builder.RunningBuild, error) {
					return <-running, nil
				}

				fakeBuilder.AttachStub = func(running builder.RunningBuild, emitter event.Emitter, abort <-chan struct{}) (builder.ExitedBuild, error) {
					select {}
				}
			})

			It("waits for it to start running and returns its running state", func() {
				scheduler.Start(build)

				drained := make(chan []ScheduledBuild)

				go func() {
					drained <- scheduler.Drain()
				}()

				Consistently(drained).ShouldNot(Receive())

				running <- builder.RunningBuild{
					Build:     build,
					ProcessID: 42,
				}

				var drainedBuilds []ScheduledBuild
				Eventually(drained).Should(Receive(&drainedBuilds))

				Ω(drainedBuilds).Should(HaveLen(1))
				Ω(drainedBuilds[0].Build).Should(Equal(build))
				Ω(drainedBuilds[0].Status).Should(Equal(builds.StatusStarted))
				Ω(drainedBuilds[0].ProcessID).Should(Equal(uint32(42)))
			})

			It("does not emit an end event, as the stream is not over", func() {
				scheduler.Start(build)

				go scheduler.Drain()

				emittedEvents, stop := subscribeToBuildEvents()
				defer close(stop)

				running <- builder.RunningBuild{
					Build:     build,
					ProcessID: 42,
				}

				Consistently(emittedEvents).ShouldNot(Receive(Equal(event.End{})))
			})

			Context("and it errors", func() {
				var errored chan error

				BeforeEach(func() {
					errored = make(chan error)

					fakeBuilder.StartStub = func(builds.Build, event.Emitter, <-chan struct{}) (builder.RunningBuild, error) {
						return builder.RunningBuild{}, <-errored
					}
				})

				It("waits for it to error and returns it in errored state", func() {
					scheduler.Start(build)

					drained := make(chan []ScheduledBuild)

					go func() {
						drained <- scheduler.Drain()
					}()

					Consistently(drained).ShouldNot(Receive())

					errored <- errors.New("oh no!")

					var drainedBuilds []ScheduledBuild
					Eventually(drained).Should(Receive(&drainedBuilds))

					Ω(drainedBuilds).Should(HaveLen(1))
					Ω(drainedBuilds[0].Build).Should(Equal(build))
					Ω(drainedBuilds[0].Status).Should(Equal(builds.StatusErrored))
				})

				It("emits an end event", func() {
					scheduler.Start(build)

					go scheduler.Drain()

					emittedEvents, stop := subscribeToBuildEvents()
					defer close(stop)

					errored <- errors.New("oh no!")

					Eventually(emittedEvents).Should(Receive(Equal(event.End{})))
				})

				It("closes the event stream", func() {
					scheduler.Start(build)

					go scheduler.Drain()

					emittedEvents, stop := subscribeToBuildEvents()
					defer close(stop)

					errored <- errors.New("oh no!")

					Eventually(emittedEvents).Should(BeClosed())
				})
			})
		})

		Context("when a build is running", func() {
			var running chan struct{}

			BeforeEach(func() {
				running = make(chan struct{})

				fakeBuilder.AttachStub = func(builder.RunningBuild, event.Emitter, <-chan struct{}) (builder.ExitedBuild, error) {
					close(running)
					select {}
				}
			})

			It("does not wait for it to finish", func() {
				scheduler.Start(build)

				Eventually(running).Should(BeClosed())

				drained := make(chan []ScheduledBuild)

				go func() {
					drained <- scheduler.Drain()
				}()

				Eventually(drained).Should(Receive(HaveLen(1)))
			})
		})

		Context("when a build is completing", func() {
			var completing chan struct{}
			var finished chan builds.Build

			BeforeEach(func() {
				completing = make(chan struct{})
				finished = make(chan builds.Build)

				fakeBuilder.FinishStub = func(builder.ExitedBuild, event.Emitter, <-chan struct{}) (builds.Build, error) {
					close(completing)
					return <-finished, nil
				}
			})

			It("waits for it to finish and returns the finished build", func() {
				scheduler.Start(build)

				Eventually(completing).Should(BeClosed())

				drained := make(chan []ScheduledBuild)

				go func() {
					drained <- scheduler.Drain()
				}()

				Consistently(drained).ShouldNot(Receive())

				endTime := startTime.Add(10 * time.Second)
				clock.CurrentTimeReturns(endTime)

				finished <- build

				var drainedBuilds []ScheduledBuild
				Eventually(drained).Should(Receive(&drainedBuilds))

				Ω(drainedBuilds).Should(HaveLen(1))
				Ω(drainedBuilds[0].Build).Should(Equal(build))
				Ω(drainedBuilds[0].Status).Should(Equal(builds.StatusSucceeded))
			})
		})

		Context("when no builds are being scheduled", func() {
			It("returns an empty slice", func() {
				Ω(scheduler.Drain()).Should(BeEmpty())
			})
		})
	})

	Describe("Subscribe", func() {
		var (
			emittedEvents <-chan event.Event
			stop          chan<- struct{}
			subscribeErr  error
		)

		JustBeforeEach(func() {
			emittedEvents, stop, subscribeErr = scheduler.Subscribe(build.Guid, 0)
		})

		Context("with an unknown build", func() {
			It("returns an error", func() {
				Ω(subscribeErr).Should(HaveOccurred())
			})
		})

		Context("with a started build", func() {
			var (
				buildEmitter <-chan event.Emitter
				finishBuild  chan<- struct{}
			)

			BeforeEach(func() {
				e := make(chan event.Emitter, 1)
				f := make(chan struct{})

				buildEmitter = e
				finishBuild = f

				fakeBuilder.AttachStub = func(running builder.RunningBuild, emitter event.Emitter, abort <-chan struct{}) (builder.ExitedBuild, error) {
					e <- emitter
					<-f
					return builder.ExitedBuild{
						Build: running.Build,
					}, nil
				}

				scheduler.Start(build)
			})

			AfterEach(func() {
				close(finishBuild)
			})

			It("does not error", func() {
				Ω(subscribeErr).ShouldNot(HaveOccurred())
			})

			It("returns its event stream, and a channel to close the subscription", func() {
				var emitter event.Emitter
				Eventually(buildEmitter).Should(Receive(&emitter))

				emitter.EmitEvent(event.Start{Time: 1})
				Eventually(emittedEvents).Should(Receive(Equal(event.Start{Time: 1})))

				close(stop)

				emitter.EmitEvent(event.Start{Time: 2})
				Consistently(emittedEvents).ShouldNot(Receive())
			})
		})
	})
})
