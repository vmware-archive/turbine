package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/room101-ci/agent/api"
	"github.com/room101-ci/agent/api/builds"
	"github.com/room101-ci/agent/api/builds/scheduler/fakescheduler"
)

var _ = Describe("API", func() {
	var scheduler *fakescheduler.FakeScheduler

	var server *httptest.Server
	var client *http.Client

	BeforeEach(func() {
		scheduler = fakescheduler.New()
		handler := api.New(NullLogger(), scheduler)

		server = httptest.NewServer(handler)
		client = &http.Client{
			Transport: &http.Transport{},
		}
	})

	Describe("POST /builds", func() {
		var build *builds.Build
		var requestBody string
		var response *http.Response

		buildPayload := func(build *builds.Build) string {
			payload, err := json.Marshal(build)
			Ω(err).ShouldNot(HaveOccurred())

			return string(payload)
		}

		BeforeEach(func() {
			build = &builds.Build{
				Guid: "abc",
				Source: builds.BuildSource{
					Type: "git",
					URI:  "https://github.com/room101-ci/agent.git",
					Ref:  "deadbeef",
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

		It("returns the build", func() {
			var returnedBuild builds.Build

			err := json.NewDecoder(response.Body).Decode(&returnedBuild)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(returnedBuild).Should(Equal(*build))
		})

		It("schedules the build", func() {
			Ω(scheduler.Scheduled()).Should(ContainElement(build))
		})

		Context("when scheduling fails", func() {
			BeforeEach(func() {
				scheduler.ScheduleError = errors.New("oh no!")
			})

			It("returns 503", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusServiceUnavailable))
			})
		})

		Context("when guid is omitted", func() {
			BeforeEach(func() {
				build.Guid = ""
				requestBody = buildPayload(build)
			})

			It("returns 400", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusBadRequest))
			})
		})

		Context("when ref is not given for a git source", func() {
			BeforeEach(func() {
				build.Source.Type = "git"
				build.Source.Ref = ""
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
