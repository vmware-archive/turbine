package scheduler_test

import (
	"errors"
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"

	"github.com/room101-ci/agent/api/builds"
	"github.com/room101-ci/agent/api/builds/builder/fakebuilder"
	. "github.com/room101-ci/agent/api/builds/scheduler"
)

var _ = Describe("Scheduler", func() {
	var builder *fakebuilder.Builder
	var scheduler *Scheduler

	BeforeEach(func() {
		builder = fakebuilder.New()
		scheduler = NewScheduler(builder)
	})

	Describe("Schedule", func() {
		var server *ghttp.Server
		var build *builds.Build

		BeforeEach(func() {
			server = ghttp.NewServer()

			build = &builds.Build{
				Guid: "abc",

				Callback: server.URL() + "/abc",

				Source: builds.BuildSource{
					Type: "git",
					URI:  "http://10.10.2.20:56789/foo.git",
					Ref:  "deadbeef",
				},

				Parameters: map[string]string{
					"FOO":  "bar",
					"FIZZ": "buzz",
				},

				SecureParameters: map[string]string{
					"USERNAME": "some-username",
					"PASSWORD": "secret",
				},
			}
		})

		It("kicks off a builder", func() {
			server.AllowUnhandledRequests = true

			err := scheduler.Schedule(build)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(builder.Built).Should(ContainElement(build))
		})

		Context("when the build succeeds", func() {
			BeforeEach(func() {
				builder.BuildResult = true

				succeededBuild := *build
				succeededBuild.Status = "succeeded"

				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("PUT", "/abc"),
						ghttp.VerifyContentType("application/json"),
						ghttp.VerifyJSONRepresenting(succeededBuild),
						ghttp.RespondWith(http.StatusOK, ""),
					),
				)
			})

			It("reports the build as succeeded", func() {
				err := scheduler.Schedule(build)
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(server.ReceivedRequests).ShouldNot(BeEmpty())
			})
		})

		Context("when the build fails", func() {
			BeforeEach(func() {
				builder.BuildResult = false

				failedBuild := *build
				failedBuild.Status = "failed"

				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("PUT", "/abc"),
						ghttp.VerifyContentType("application/json"),
						ghttp.VerifyJSONRepresenting(failedBuild),
						ghttp.RespondWith(http.StatusOK, ""),
					),
				)
			})

			It("reports the build as failed", func() {
				err := scheduler.Schedule(build)
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(server.ReceivedRequests).ShouldNot(BeEmpty())
			})
		})

		Context("when building fails", func() {
			BeforeEach(func() {
				builder.BuildError = errors.New("oh no!")

				erroredBuild := *build
				erroredBuild.Status = "errored"

				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("PUT", "/abc"),
						ghttp.VerifyContentType("application/json"),
						ghttp.VerifyJSONRepresenting(erroredBuild),
						ghttp.RespondWith(http.StatusOK, ""),
					),
				)
			})

			It("reports the build as errored", func() {
				err := scheduler.Schedule(build)
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(server.ReceivedRequests).ShouldNot(BeEmpty())
			})
		})
	})
})
