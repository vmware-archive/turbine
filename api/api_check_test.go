package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/resource/fakes"
)

var _ = Describe("POST /checks", func() {
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

	Context("when initializing the resource succeeds", func() {
		var resource *fakes.FakeResource

		BeforeEach(func() {
			resource = new(fakes.FakeResource)

			tracker.InitReturns(resource, nil)
		})

		It("returns 200", func() {
			Ω(response.StatusCode).Should(Equal(http.StatusOK))
		})

		It("checks for new versions of the given input", func() {
			Ω(resource.CheckCallCount()).Should(Equal(1))
			Ω(resource.CheckArgsForCall(0)).Should(Equal(input))
		})

		It("releases the resource", func() {
			Ω(tracker.ReleaseCallCount()).Should(Equal(1))
			Ω(tracker.ReleaseArgsForCall(0)).Should(Equal(resource))
		})

		Context("when the check returns versions", func() {
			var version1 builds.Version
			var version2 builds.Version

			BeforeEach(func() {
				version1 = builds.Version{"ref": "a"}
				version2 = builds.Version{"ref": "b"}

				resource.CheckReturns([]builds.Version{version1, version2}, nil)
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
				resource.CheckReturns(nil, errors.New("oh no!"))
			})

			It("returns 500", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusInternalServerError))
			})
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
