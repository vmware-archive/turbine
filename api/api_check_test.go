package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/winston-ci/prole/api"
	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/checker/fakechecker"
	"github.com/winston-ci/prole/scheduler/fakescheduler"
)

var _ = Describe("API", func() {
	var checker *fakechecker.FakeChecker

	var server *httptest.Server
	var client *http.Client

	BeforeEach(func() {
		scheduler := fakescheduler.New()
		checker = fakechecker.New()

		handler, err := api.New(scheduler, checker)
		Ω(err).ShouldNot(HaveOccurred())

		server = httptest.NewServer(handler)
		client = &http.Client{
			Transport: &http.Transport{},
		}
	})

	Describe("POST /checks", func() {
		var input builds.Input
		var requestBody string
		var response *http.Response

		inputPayload := func(input builds.Input) string {
			payload, err := json.Marshal(input)
			Ω(err).ShouldNot(HaveOccurred())

			return string(payload)
		}

		BeforeEach(func() {
			input = builds.Input{
				Type:    "git",
				Source:  builds.Source{"uri": "example.com"},
				Version: builds.Version{"ref": "foo"},
			}

			requestBody = inputPayload(input)
		})

		JustBeforeEach(func() {
			var err error

			response, err = client.Post(
				server.URL+"/checks",
				"application/json",
				bytes.NewBufferString(requestBody),
			)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("returns 200", func() {
			Ω(response.StatusCode).Should(Equal(http.StatusOK))
		})

		It("checks for new versions of the given input", func() {
			Ω(checker.Checked()).Should(ContainElement(input))
		})

		Context("when the check returns versions", func() {
			var version1 builds.Version
			var version2 builds.Version

			BeforeEach(func() {
				version1 = builds.Version{"ref": "a"}
				version2 = builds.Version{"ref": "b"}
				checker.CheckResult = []builds.Version{version1, version2}
			})

			It("responds with them", func() {
				var versions []builds.Version
				err := json.NewDecoder(response.Body).Decode(&versions)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(versions).Should(Equal([]builds.Version{version1, version2}))
			})
		})

		Context("when the check fails", func() {
			BeforeEach(func() {
				checker.CheckError = errors.New("oh no!")
			})

			It("returns 500", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusInternalServerError))
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
