package scheduler_test

import (
	"errors"
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/builder/fakebuilder"
	. "github.com/winston-ci/prole/scheduler"
)

var _ = Describe("Scheduler", func() {
	var builder *fakebuilder.Builder
	var scheduler Scheduler

	BeforeEach(func() {
		builder = fakebuilder.New()
		scheduler = NewScheduler(builder)
	})

	Describe("Schedule", func() {
		var server *ghttp.Server
		var build builds.Build

		BeforeEach(func() {
			server = ghttp.NewServer()

			build = builds.Build{
				Guid: "abc",

				Inputs: []builds.Input{
					{
						Type: "git",
					},
				},

				Config: builds.Config{
					Env: [][2]string{
						{"FOO", "bar"},
						{"FIZZ", "buzz"},
					},
				},
			}
		})

		handleBuild := func(build builds.Build) <-chan struct{} {
			gotRequest := make(chan struct{})

			server.AppendHandlers(
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

		It("kicks off a builder", func() {
			err := scheduler.Schedule(build)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(builder.Built).Should(ContainElement(build))
		})

		Context("when there is a callback registered", func() {
			BeforeEach(func() {
				build.Callback = server.URL() + "/abc"
			})

			itRetries := func(index int, assertion func()) {
				Context("and the callback URI fails", func() {
					BeforeEach(func() {
						handler := server.GetHandler(index)

						server.SetHandler(index, func(w http.ResponseWriter, r *http.Request) {
							server.HTTPTestServer.CloseClientConnections()
						})

						server.AppendHandlers(handler)
					})

					It("retries", func() {
						// ignore completion callback
						server.AllowUnhandledRequests = true

						err := scheduler.Schedule(build)
						Ω(err).ShouldNot(HaveOccurred())

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

					builder.StartedBuild = &startedBuild

					gotStartedCallback = handleBuild(startedBuild)
				})

				It("reports the started build as started", func() {
					// ignore completion callback
					server.AllowUnhandledRequests = true

					err := scheduler.Schedule(build)
					Ω(err).ShouldNot(HaveOccurred())

					Eventually(gotStartedCallback).Should(BeClosed())
				})

				itRetries(0, func() {
					Eventually(gotStartedCallback, 3).Should(BeClosed())
				})

				Context("when the build succeeds", func() {
					var gotRequest <-chan struct{}

					BeforeEach(func() {
						builder.FinishedBuild = startedBuild

						succeededBuild := startedBuild
						succeededBuild.Status = builds.StatusSucceeded

						gotRequest = handleBuild(succeededBuild)
					})

					It("reports the started build as succeeded", func() {
						err := scheduler.Schedule(build)
						Ω(err).ShouldNot(HaveOccurred())

						Eventually(gotRequest).Should(BeClosed())
					})

					itRetries(1, func() {
						Eventually(gotRequest, 3).Should(BeClosed())
					})
				})

				Context("when the build fails", func() {
					var gotRequest <-chan struct{}

					BeforeEach(func() {
						builder.BuildFailure = errors.New("exit status 1")

						failedBuild := startedBuild
						failedBuild.Status = builds.StatusFailed

						gotRequest = handleBuild(failedBuild)
					})

					It("reports the build as failed", func() {
						err := scheduler.Schedule(build)
						Ω(err).ShouldNot(HaveOccurred())

						Eventually(gotRequest).Should(BeClosed())
					})

					itRetries(1, func() {
						Eventually(gotRequest, 3).Should(BeClosed())
					})
				})

				Context("when building fails", func() {
					var gotRequest <-chan struct{}

					BeforeEach(func() {
						builder.BuildError = errors.New("oh no!")

						erroredBuild := startedBuild
						erroredBuild.Status = builds.StatusErrored

						gotRequest = handleBuild(erroredBuild)
					})

					It("reports the build as errored", func() {
						err := scheduler.Schedule(build)
						Ω(err).ShouldNot(HaveOccurred())

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
					builder.StartError = errors.New("oh no!")

					erroredBuild := build
					erroredBuild.Status = builds.StatusErrored

					gotRequest = handleBuild(erroredBuild)
				})

				It("reports the build as errored", func() {
					err := scheduler.Schedule(build)
					Ω(err).ShouldNot(HaveOccurred())

					Eventually(gotRequest).Should(BeClosed())
				})

				itRetries(0, func() {
					Eventually(gotRequest, 3).Should(BeClosed())
				})
			})
		})
	})
})
