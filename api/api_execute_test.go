package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/router"

	"github.com/winston-ci/prole/api"
	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/checker/fakechecker"
	"github.com/winston-ci/prole/routes"
	"github.com/winston-ci/prole/scheduler/fakescheduler"
)

var _ = Describe("API", func() {
	var scheduler *fakescheduler.FakeScheduler

	var server *httptest.Server
	var client *http.Client

	BeforeEach(func() {
		scheduler = fakescheduler.New()
		checker := fakechecker.New()

		proleEndpoint := router.NewRequestGenerator("http://some-prole", routes.Routes)

		handler, err := api.New(scheduler, checker, proleEndpoint)
		Ω(err).ShouldNot(HaveOccurred())

		server = httptest.NewServer(handler)
		client = &http.Client{
			Transport: &http.Transport{},
		}
	})

	Describe("POST /builds", func() {
		var build builds.Build
		var requestBody string
		var response *http.Response

		buildPayload := func(build builds.Build) string {
			payload, err := json.Marshal(build)
			Ω(err).ShouldNot(HaveOccurred())

			return string(payload)
		}

		BeforeEach(func() {
			build = builds.Build{
				Inputs: []builds.Input{
					{
						Type: "git",
					},
				},
			}

			requestBody = buildPayload(build)
		})

		JustBeforeEach(func() {
			var err error

			response, err = client.Post(
				server.URL+"/builds",
				"application/json",
				bytes.NewBufferString(requestBody),
			)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns 201", func() {
			Ω(response.StatusCode).Should(Equal(http.StatusCreated))
		})

		It("schedules the build and returns it with a guid and abort url", func() {
			var returnedBuild builds.Build

			err := json.NewDecoder(response.Body).Decode(&returnedBuild)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(returnedBuild.Guid).ShouldNot(BeEmpty())
			Ω(returnedBuild.AbortURL).Should(Equal("http://some-prole/builds/" + returnedBuild.Guid + "/abort"))
			Ω(returnedBuild.Inputs).Should(Equal(build.Inputs))

			Ω(scheduler.Scheduled()).Should(ContainElement(returnedBuild))
		})

		//Describe("the abort url returned", func() {
		//It("aborts the build", func() {
		//var returnedBuild builds.Build

		//err := json.NewDecoder(response.Body).Decode(&returnedBuild)
		//Ω(err).ShouldNot(HaveOccurred())

		//response, err := client.Post(
		//returnedBuild.AbortURL,
		//"application/json",
		//nil,
		//)
		//Ω(err).ShouldNot(HaveOccurred())

		//Ω(response.StatusCode).Should(Equal(http.StatusOK))

		//Ω(scheduler.Aborted()).Should(ContainElement(returnedBuild.Guid))
		//})
		//})

		Context("when the callback url is malformed", func() {
			BeforeEach(func() {
				build.Callback = "ß"
				requestBody = buildPayload(build)
			})

			It("returns 400", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusBadRequest))
			})
		})

		Context("when the payload is malformed JSON", func() {
			BeforeEach(func() {
				requestBody = "ß"
			})

			It("returns 400", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusBadRequest))
			})
		})
	})
})
