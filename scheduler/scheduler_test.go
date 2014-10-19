package scheduler_test

import (
	"bytes"
	"errors"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
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
	})

	subscribeToBuildEvents := func() (<-chan event.Event, chan<- struct{}) {
		events, versions, stop, err := scheduler.Subscribe(build.Guid, 0)
		Ω(err).ShouldNot(HaveOccurred())

		go func() {
			for _ = range versions {
				// discard versions to unblock events
			}
		}()

		return events, stop
	}

	Describe("Start", func() {
		var callbackServer *ghttp.Server

		BeforeEach(func() {
			callbackServer = ghttp.NewServer()
			build.StatusCallback = callbackServer.URL() + "/abc"
		})

		AfterEach(func() {
			callbackServer.Close()
		})

		handleBuild := func(build builds.Build) <-chan struct{} {
			gotRequest := make(chan struct{})

			callbackServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("PUT", "/abc"),
					ghttp.VerifyJSONRepresenting(build),
					ghttp.RespondWith(http.StatusOK, ""),
					func(http.ResponseWriter, *http.Request) {
						close(gotRequest)
					},
				),
			)

			return gotRequest
		}

		itRetries := func(index int, assertion func()) {
			Context("and the callback URI fails", func() {
				BeforeEach(func() {
					handler := callbackServer.GetHandler(index)

					callbackServer.SetHandler(index, func(w http.ResponseWriter, r *http.Request) {
						callbackServer.HTTPTestServer.CloseClientConnections()
					})

					callbackServer.AppendHandlers(handler)
				})

				It("retries", func() {
					// ignore completion callback
					callbackServer.AllowUnhandledRequests = true

					scheduler.Start(build)

					assertion()
				})
			})
		}

		It("kicks off a builder", func() {
			scheduler.Start(build)

			Eventually(fakeBuilder.StartCallCount).Should(Equal(1))

			startedBuild, _, _ := fakeBuilder.StartArgsForCall(0)
			Ω(startedBuild).Should(Equal(build))
		})

		It("emits the current event version", func() {
			scheduler.Start(build)

			_, versions, stop, err := scheduler.Subscribe(build.Guid, 0)
			Ω(err).ShouldNot(HaveOccurred())

			defer close(stop)

			Eventually(versions).Should(Receive(Equal(event.CURRENT_VERSION)))
		})

		Context("and the build starts", func() {
			var running builder.RunningBuild
			var gotStartedCallback <-chan struct{}

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

				running.Build.Status = builds.StatusStarted
				running.Build.StartTime = startTime.Unix()

				fakeBuilder.StartReturns(running, nil)

				gotStartedCallback = handleBuild(running.Build)
			})

			It("reports the started build as started", func() {
				// ignore completion callback
				callbackServer.AllowUnhandledRequests = true

				scheduler.Start(build)

				Eventually(gotStartedCallback).Should(BeClosed())
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

			itRetries(0, func() {
				Eventually(gotStartedCallback, 3).Should(BeClosed())
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
					var gotRequest <-chan struct{}

					BeforeEach(func() {
						fakeBuilder.FinishStub = func(builder.ExitedBuild, event.Emitter, <-chan struct{}) (builds.Build, error) {
							clock.CurrentTimeReturns(endTime)
							return exited.Build, nil
						}

						succeededBuild := exited.Build
						succeededBuild.Status = builds.StatusSucceeded
						succeededBuild.StartTime = startTime.Unix()
						succeededBuild.EndTime = endTime.Unix()

						gotRequest = handleBuild(succeededBuild)
					})

					It("reports the started build as succeeded", func() {
						scheduler.Start(build)

						Eventually(gotRequest).Should(BeClosed())
					})

					It("emits a succeeded status event", func() {
						scheduler.Start(build)

						emittedEvents, stop := subscribeToBuildEvents()
						defer close(stop)

						Eventually(emittedEvents).Should(Receive(Equal(event.Status{
							Status: builds.StatusSucceeded,
							Time:   endTime.Unix(),
						})))
					})

					itRetries(1, func() {
						Eventually(gotRequest, 3).Should(BeClosed())
					})
				})

				Context("and the build fails to finish", func() {
					var gotRequest <-chan struct{}

					BeforeEach(func() {
						fakeBuilder.FinishStub = func(builder.ExitedBuild, event.Emitter, <-chan struct{}) (builds.Build, error) {
							clock.CurrentTimeReturns(endTime)
							return builds.Build{}, errors.New("oh no!")
						}

						erroredBuild := exited.Build
						erroredBuild.Status = builds.StatusErrored
						erroredBuild.StartTime = startTime.Unix()
						erroredBuild.EndTime = endTime.Unix()

						gotRequest = handleBuild(erroredBuild)
					})

					It("reports the started build as errored", func() {
						scheduler.Start(build)

						Eventually(gotRequest).Should(BeClosed())
					})

					It("emits an errored status event", func() {
						scheduler.Start(build)

						emittedEvents, stop := subscribeToBuildEvents()
						defer close(stop)

						Eventually(emittedEvents).Should(Receive(Equal(event.Status{
							Status: builds.StatusErrored,
							Time:   endTime.Unix(),
						})))
					})

					itRetries(1, func() {
						Eventually(gotRequest, 3).Should(BeClosed())
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
					var gotRequest <-chan struct{}

					BeforeEach(func() {
						fakeBuilder.FinishStub = func(builder.ExitedBuild, event.Emitter, <-chan struct{}) (builds.Build, error) {
							clock.CurrentTimeReturns(endTime)
							return exited.Build, nil
						}

						failedBuild := exited.Build
						failedBuild.Status = builds.StatusFailed
						failedBuild.EndTime = endTime.Unix()

						gotRequest = handleBuild(failedBuild)
					})

					It("reports the started build as failed", func() {
						scheduler.Start(build)

						Eventually(gotRequest).Should(BeClosed())
					})

					It("emits a failed status event", func() {
						scheduler.Start(build)

						emittedEvents, stop := subscribeToBuildEvents()
						defer close(stop)

						Eventually(emittedEvents).Should(Receive(Equal(event.Status{
							Status: builds.StatusFailed,
							Time:   endTime.Unix(),
						})))
					})

					itRetries(1, func() {
						Eventually(gotRequest, 3).Should(BeClosed())
					})
				})

				Context("and the build fails to finish", func() {
					var gotRequest <-chan struct{}

					BeforeEach(func() {
						fakeBuilder.FinishStub = func(builder.ExitedBuild, event.Emitter, <-chan struct{}) (builds.Build, error) {
							clock.CurrentTimeReturns(endTime)
							return exited.Build, errors.New("oh no!")
						}

						erroredBuild := exited.Build
						erroredBuild.Status = builds.StatusErrored
						erroredBuild.EndTime = endTime.Unix()

						gotRequest = handleBuild(erroredBuild)
					})

					It("reports the started build as errored", func() {
						scheduler.Start(build)

						Eventually(gotRequest).Should(BeClosed())
					})

					It("emits an errored status event", func() {
						scheduler.Start(build)

						emittedEvents, stop := subscribeToBuildEvents()
						defer close(stop)

						Eventually(emittedEvents).Should(Receive(Equal(event.Status{
							Status: builds.StatusErrored,
							Time:   endTime.Unix(),
						})))
					})

					itRetries(1, func() {
						Eventually(gotRequest, 3).Should(BeClosed())
					})
				})
			})

			Context("when building fails", func() {
				var gotRequest <-chan struct{}

				BeforeEach(func() {
					fakeBuilder.AttachStub = func(builder.RunningBuild, event.Emitter, <-chan struct{}) (builder.ExitedBuild, error) {
						clock.CurrentTimeReturns(endTime)
						return builder.ExitedBuild{}, errors.New("oh no!")
					}

					erroredBuild := running.Build
					erroredBuild.Status = builds.StatusErrored
					erroredBuild.EndTime = endTime.Unix()

					gotRequest = handleBuild(erroredBuild)
				})

				It("reports the build as errored", func() {
					scheduler.Start(build)

					Eventually(gotRequest).Should(BeClosed())
				})

				It("emits an errored status event", func() {
					scheduler.Start(build)

					emittedEvents, stop := subscribeToBuildEvents()
					defer close(stop)

					Eventually(emittedEvents).Should(Receive(Equal(event.Status{
						Status: builds.StatusErrored,
						Time:   endTime.Unix(),
					})))
				})

				itRetries(1, func() {
					Eventually(gotRequest, 3).Should(BeClosed())
				})
			})
		})

		Context("and the build fails to start", func() {
			var startTime time.Time
			var endTime time.Time
			var gotRequest <-chan struct{}

			BeforeEach(func() {
				startTime = time.Now()
				endTime = startTime

				clock.CurrentTimeReturns(startTime)

				fakeBuilder.StartStub = func(builds.Build, event.Emitter, <-chan struct{}) (builder.RunningBuild, error) {
					clock.CurrentTimeReturns(endTime)
					return builder.RunningBuild{}, errors.New("oh no!")
				}

				erroredBuild := build
				erroredBuild.Status = builds.StatusErrored
				erroredBuild.StartTime = startTime.Unix()
				erroredBuild.EndTime = endTime.Unix()

				gotRequest = handleBuild(erroredBuild)
			})

			It("reports the build as errored", func() {
				scheduler.Start(build)

				Eventually(gotRequest).Should(BeClosed())
			})

			It("emits an errored status event", func() {
				scheduler.Start(build)

				emittedEvents, stop := subscribeToBuildEvents()
				defer close(stop)

				Eventually(emittedEvents).Should(Receive(Equal(event.Status{
					Status: builds.StatusErrored,
					Time:   endTime.Unix(),
				})))
			})

			itRetries(0, func() {
				Eventually(gotRequest, 3).Should(BeClosed())
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

			It("emits an aborted status event", func() {
				scheduler.Start(build)

				Eventually(gotAborting).Should(Receive())

				scheduler.Abort(build.Guid)

				emittedEvents, stop := subscribeToBuildEvents()
				defer close(stop)

				Eventually(emittedEvents).Should(Receive(Equal(event.Status{
					Status: builds.StatusAborted,
					Time:   currentTime.Unix(),
				})))
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

			It("emits an aborted status event", func() {
				scheduler.Start(build)

				Eventually(gotAborting).Should(Receive())

				scheduler.Abort(build.Guid)

				emittedEvents, stop := subscribeToBuildEvents()
				defer close(stop)

				Eventually(emittedEvents).Should(Receive(Equal(event.Status{
					Status: builds.StatusAborted,
					Time:   currentTime.Unix(),
				})))
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

			It("emits an aborted status event", func() {
				scheduler.Start(build)

				Eventually(gotAborting).Should(Receive())

				scheduler.Abort(build.Guid)

				emittedEvents, stop := subscribeToBuildEvents()
				defer close(stop)

				Eventually(emittedEvents).Should(Receive(Equal(event.Status{
					Status: builds.StatusAborted,
					Time:   currentTime.Unix(),
				})))
			})
		})
	})

	Describe("Drain", func() {
		Context("when a build is starting", func() {
			startingBuild := builds.Build{
				Guid: "starting",
			}

			var startTime time.Time
			var running chan builder.RunningBuild

			BeforeEach(func() {
				startTime = time.Now()
				clock.CurrentTimeReturns(startTime)

				running = make(chan builder.RunningBuild)

				fakeBuilder.StartStub = func(build builds.Build, emitter event.Emitter, abort <-chan struct{}) (builder.RunningBuild, error) {
					if build.Guid == "starting" {
						return <-running, nil
					}

					return builder.RunningBuild{
						Build: build,
					}, nil
				}

				fakeBuilder.AttachStub = func(running builder.RunningBuild, emitter event.Emitter, abort <-chan struct{}) (builder.ExitedBuild, error) {
					select {}
				}
			})

			It("waits for it to start running and returns its running state", func() {
				scheduler.Start(startingBuild)

				startedBuild := startingBuild
				startedBuild.Status = builds.StatusStarted
				startedBuild.StartTime = startTime.Unix()
				runningBuild := builder.RunningBuild{Build: startedBuild}

				drained := make(chan []builder.RunningBuild)

				go func() {
					drained <- scheduler.Drain()
				}()

				Consistently(drained).ShouldNot(Receive())

				running <- runningBuild

				Eventually(drained).Should(Receive(Equal([]builder.RunningBuild{runningBuild})))
			})

			Context("and it errors", func() {
				var errored chan error

				BeforeEach(func() {
					errored = make(chan error)

					fakeBuilder.StartStub = func(builds.Build, event.Emitter, <-chan struct{}) (builder.RunningBuild, error) {
						return builder.RunningBuild{}, <-errored
					}
				})

				It("waits for it to error and does not return it", func() {
					scheduler.Start(build)

					drained := make(chan []builder.RunningBuild)

					go func() {
						drained <- scheduler.Drain()
					}()

					Consistently(drained).ShouldNot(Receive())

					errored <- errors.New("oh no!")

					Eventually(drained).Should(Receive(BeEmpty()))
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

				drained := make(chan []builder.RunningBuild)

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

			It("waits for it to error and does not return the finished build", func() {
				scheduler.Start(build)

				Eventually(completing).Should(BeClosed())

				drained := make(chan []builder.RunningBuild)

				go func() {
					drained <- scheduler.Drain()
				}()

				Consistently(drained).ShouldNot(Receive())

				finished <- builds.Build{}

				Eventually(drained).Should(Receive(BeEmpty()))
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
			emittedEvents   <-chan event.Event
			emittedVersions <-chan event.Version
			stop            chan<- struct{}
			subscribeErr    error
		)

		JustBeforeEach(func() {
			emittedEvents, emittedVersions, stop, subscribeErr = scheduler.Subscribe(build.Guid, 0)
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

			It("returns its event and version streams, and a channel to close the subscription", func() {
				var emitter event.Emitter
				Eventually(buildEmitter).Should(Receive(&emitter))

				Eventually(emittedVersions).Should(Receive(Equal(event.CURRENT_VERSION)))

				emitter.EmitEvent(event.Start{Time: 1})
				Eventually(emittedEvents).Should(Receive(Equal(event.Start{Time: 1})))

				close(stop)

				emitter.EmitEvent(event.Start{Time: 2})
				Consistently(emittedEvents).ShouldNot(Receive())
			})
		})
	})
})
