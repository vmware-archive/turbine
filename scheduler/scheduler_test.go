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

			Context("and the build starts", func() {
				var startedBuild builds.Build

				BeforeEach(func() {
					startedBuild = build
					startedBuild.Config.Image = "some-reconfigured-image"

					builder.StartedBuild = &startedBuild
				})

				Context("when the build succeeds", func() {
					var gotRequest <-chan struct{}

					BeforeEach(func() {
						builder.BuildResult = true

						succeededBuild := startedBuild
						succeededBuild.Status = "succeeded"

						gotRequest = handleBuild(succeededBuild)
					})

					It("reports the started build as succeeded", func() {
						err := scheduler.Schedule(build)
						Ω(err).ShouldNot(HaveOccurred())

						Eventually(gotRequest).Should(BeClosed())
					})
				})

				Context("when the build fails", func() {
					var gotRequest <-chan struct{}

					BeforeEach(func() {
						builder.BuildResult = false

						failedBuild := startedBuild
						failedBuild.Status = "failed"

						gotRequest = handleBuild(failedBuild)
					})

					It("reports the build as failed", func() {
						err := scheduler.Schedule(build)
						Ω(err).ShouldNot(HaveOccurred())

						Eventually(gotRequest).Should(BeClosed())
					})
				})

				Context("when building fails", func() {
					var gotRequest <-chan struct{}

					BeforeEach(func() {
						builder.BuildError = errors.New("oh no!")

						erroredBuild := startedBuild
						erroredBuild.Status = "errored"

						gotRequest = handleBuild(erroredBuild)
					})

					It("reports the build as errored", func() {
						err := scheduler.Schedule(build)
						Ω(err).ShouldNot(HaveOccurred())

						Eventually(gotRequest).Should(BeClosed())
					})
				})
			})
		})
	})
})
