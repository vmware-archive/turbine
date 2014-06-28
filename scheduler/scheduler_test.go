package scheduler_test

import (
	"errors"
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/pivotal-golang/lager/lagertest"

	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/builder"
	"github.com/concourse/turbine/builder/fakebuilder"
	. "github.com/concourse/turbine/scheduler"
)

var _ = Describe("Scheduler", func() {
	var fakeBuilder *fakebuilder.Builder
	var scheduler Scheduler

	var build builds.Build

	BeforeEach(func() {
		fakeBuilder = fakebuilder.New()
		scheduler = NewScheduler(lagertest.NewTestLogger("test"), fakeBuilder)

		build = builds.Build{
			Guid: "abc",

			Inputs: []builds.Input{
				{
					Type: "git",
				},
			},

			Config: builds.Config{
				Env: []map[string]string{
					{"FOO": "bar"},
					{"FIZZ": "buzz"},
				},
			},
		}
	})

	Describe("Schedule", func() {
		It("kicks off a builder", func() {
			scheduler.Start(build)

			Eventually(fakeBuilder.Built).Should(ContainElement(build))
		})

		Context("when there is a callback registered", func() {
			var callbackServer *ghttp.Server

			BeforeEach(func() {
				callbackServer = ghttp.NewServer()
				build.Callback = callbackServer.URL() + "/abc"
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

			Context("and the build starts", func() {
				var startedBuild builds.Build

				var gotStartedCallback <-chan struct{}

				BeforeEach(func() {
					startedBuild = build
					startedBuild.Config.Image = "some-reconfigured-image"
					startedBuild.Status = builds.StatusStarted

					fakeBuilder.StartedBuild = &startedBuild

					gotStartedCallback = handleBuild(startedBuild)
				})

				It("reports the started build as started", func() {
					// ignore completion callback
					callbackServer.AllowUnhandledRequests = true

					scheduler.Start(build)

					Eventually(gotStartedCallback).Should(BeClosed())
				})

				itRetries(0, func() {
					Eventually(gotStartedCallback, 3).Should(BeClosed())
				})

				Context("when the build succeeds", func() {
					var gotRequest <-chan struct{}

					BeforeEach(func() {
						fakeBuilder.SucceededBuild = &startedBuild
					})

					It("completes the build", func() {
						scheduler.Start(build)

						Eventually(fakeBuilder.Completed).Should(Equal([]builder.SucceededBuild{
							{
								Build: startedBuild,
							},
						}))
					})

					Context("and the build completes", func() {
						BeforeEach(func() {
							fakeBuilder.FinishedBuild = startedBuild

							succeededBuild := startedBuild
							succeededBuild.Status = builds.StatusSucceeded

							gotRequest = handleBuild(succeededBuild)
						})

						It("reports the started build as succeeded", func() {
							scheduler.Start(build)

							Eventually(gotRequest).Should(BeClosed())
						})

						itRetries(1, func() {
							Eventually(gotRequest, 3).Should(BeClosed())
						})
					})

					Context("and the build fails to complete", func() {
						BeforeEach(func() {
							fakeBuilder.CompleteError = errors.New("oh no!")

							erroredBuild := startedBuild
							erroredBuild.Status = builds.StatusErrored

							gotRequest = handleBuild(erroredBuild)
						})

						It("reports the started build as errored", func() {
							scheduler.Start(build)

							Eventually(gotRequest).Should(BeClosed())
						})

						itRetries(1, func() {
							Eventually(gotRequest, 3).Should(BeClosed())
						})
					})
				})

				Context("when the build fails", func() {
					var gotRequest <-chan struct{}

					BeforeEach(func() {
						fakeBuilder.BuildFailure = errors.New("exit status 1")

						failedBuild := startedBuild
						failedBuild.Status = builds.StatusFailed

						gotRequest = handleBuild(failedBuild)
					})

					It("reports the build as failed", func() {
						scheduler.Start(build)

						Eventually(gotRequest).Should(BeClosed())
					})

					itRetries(1, func() {
						Eventually(gotRequest, 3).Should(BeClosed())
					})
				})

				Context("when building fails", func() {
					var gotRequest <-chan struct{}

					BeforeEach(func() {
						fakeBuilder.BuildError = errors.New("oh no!")

						erroredBuild := startedBuild
						erroredBuild.Status = builds.StatusErrored

						gotRequest = handleBuild(erroredBuild)
					})

					It("reports the build as errored", func() {
						scheduler.Start(build)

						Eventually(gotRequest).Should(BeClosed())
					})

					itRetries(1, func() {
						Eventually(gotRequest, 3).Should(BeClosed())
					})
				})
			})

			Context("and the build fails to start", func() {
				var gotRequest <-chan struct{}

				BeforeEach(func() {
					fakeBuilder.StartError = errors.New("oh no!")

					erroredBuild := build
					erroredBuild.Status = builds.StatusErrored

					gotRequest = handleBuild(erroredBuild)
				})

				It("reports the build as errored", func() {
					scheduler.Start(build)

					Eventually(gotRequest).Should(BeClosed())
				})

				itRetries(0, func() {
					Eventually(gotRequest, 3).Should(BeClosed())
				})
			})
		})
	})

	Describe("Abort", func() {
		Context("when starting a build", func() {
			var gotAborting chan (<-chan struct{})

			BeforeEach(func() {
				gotAborting = make(chan (<-chan struct{}), 1)

				fakeBuilder.WhenStarting = func(build builds.Build, abort <-chan struct{}) (<-chan builder.RunningBuild, <-chan error) {
					gotAborting <- abort
					return nil, nil
				}
			})

			It("signals to the builder to abort", func() {
				scheduler.Start(build)

				var abort <-chan struct{}
				Ω(gotAborting).Should(Receive(&abort))

				scheduler.Abort(build.Guid)

				Ω(abort).Should(BeClosed())
			})
		})

		Context("when attached to a build", func() {
			var gotAborting chan (<-chan struct{})

			BeforeEach(func() {
				gotAborting = make(chan (<-chan struct{}), 1)

				fakeBuilder.WhenAttaching = func(build builder.RunningBuild, abort <-chan struct{}) (<-chan builder.SucceededBuild, <-chan error, <-chan error) {
					gotAborting <- abort
					return nil, nil, nil
				}
			})

			It("signals to the builder to abort", func() {
				scheduler.Start(build)

				var abort <-chan struct{}
				Eventually(gotAborting).Should(Receive(&abort))

				scheduler.Abort(build.Guid)

				Ω(abort).Should(BeClosed())
			})
		})

		Context("when completing a build", func() {
			var gotAborting chan (<-chan struct{})

			BeforeEach(func() {
				gotAborting = make(chan (<-chan struct{}), 1)

				fakeBuilder.WhenCompleting = func(build builder.SucceededBuild, abort <-chan struct{}) (<-chan builds.Build, <-chan error) {
					gotAborting <- abort
					return nil, nil
				}
			})

			It("signals to the builder to abort", func() {
				scheduler.Start(build)

				var abort <-chan struct{}
				Eventually(gotAborting).Should(Receive(&abort))

				scheduler.Abort(build.Guid)

				Ω(abort).Should(BeClosed())
			})
		})
	})

	Describe("Drain", func() {
		Context("when a build is starting", func() {
			startingBuild := builds.Build{
				Guid: "starting",
			}

			var running chan builder.RunningBuild

			BeforeEach(func() {
				running = make(chan builder.RunningBuild)

				fakeBuilder.WhenStarting = func(build builds.Build, abort <-chan struct{}) (<-chan builder.RunningBuild, <-chan error) {
					if build.Guid == "starting" {
						return running, nil
					} else {
						instantlyRunning := make(chan builder.RunningBuild, 1)

						instantlyRunning <- builder.RunningBuild{
							Build: build,
						}

						return instantlyRunning, nil
					}
				}
			})

			It("waits for it to start running and returns its running state", func() {
				scheduler.Start(startingBuild)

				startedBuild := startingBuild
				startedBuild.Status = builds.StatusStarted
				runningBuild := builder.RunningBuild{Build: startedBuild}

				drained := make(chan []builder.RunningBuild)

				go func() {
					drained <- scheduler.Drain()
				}()

				Consistently(drained).ShouldNot(Receive())

				running <- runningBuild

				Eventually(drained).Should(Receive(Equal([]builder.RunningBuild{runningBuild})))
			})

			Context("and another running build starts completing", func() {
				completingBuild := builds.Build{
					Guid: "completing",
				}

				var completing chan struct{}
				var finished chan builds.Build

				BeforeEach(func() {
					scheduler.Start(startingBuild)

					completing = make(chan struct{})
					finished = make(chan builds.Build)

					fakeBuilder.WhenCompleting = func(succeeded builder.SucceededBuild, abort <-chan struct{}) (<-chan builds.Build, <-chan error) {
						if succeeded.Build.Guid == "completing" {
							close(completing)
							return finished, nil
						} else {
							instantlyFinished := make(chan builds.Build, 1)
							instantlyFinished <- succeeded.Build
							return nil, nil
						}
					}

					fakeBuilder.WhenAttaching = func(running builder.RunningBuild, abort <-chan struct{}) (<-chan builder.SucceededBuild, <-chan error, <-chan error) {
						if running.Build.Guid == "completing" {
							instantlySucceeded := make(chan builder.SucceededBuild, 1)
							instantlySucceeded <- builder.SucceededBuild{Build: running.Build}
							return instantlySucceeded, nil, nil
						} else {
							return nil, nil, nil
						}
					}
				})

				It("waits for it to complete", func() {
					scheduler.Start(completingBuild)

					startedBuild := startingBuild
					startedBuild.Status = builds.StatusStarted
					runningBuild := builder.RunningBuild{Build: startedBuild}

					drained := make(chan []builder.RunningBuild)

					go func() {
						drained <- scheduler.Drain()
					}()

					Consistently(drained).ShouldNot(Receive())

					running <- runningBuild

					Eventually(completing).Should(BeClosed())

					Consistently(drained).ShouldNot(Receive())

					finished <- completingBuild

					Eventually(drained).Should(Receive(Equal([]builder.RunningBuild{runningBuild})))
				})
			})

			Context("and it errors", func() {
				var errored chan error

				BeforeEach(func() {
					errored = make(chan error)

					fakeBuilder.WhenStarting = func(builds.Build, <-chan struct{}) (<-chan builder.RunningBuild, <-chan error) {
						return nil, errored
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
			var succeeded chan builder.SucceededBuild
			var failed chan error
			var errored chan error

			BeforeEach(func() {
				running = make(chan struct{})
				succeeded = make(chan builder.SucceededBuild)
				failed = make(chan error)
				errored = make(chan error)

				fakeBuilder.WhenAttaching = func(builder.RunningBuild, <-chan struct{}) (<-chan builder.SucceededBuild, <-chan error, <-chan error) {
					close(running)
					return succeeded, failed, errored
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

				fakeBuilder.WhenCompleting = func(builder.SucceededBuild, <-chan struct{}) (<-chan builds.Build, <-chan error) {
					close(completing)
					return finished, nil
				}
			})

			It("waits for it to error and does not return the completed build", func() {
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
})
