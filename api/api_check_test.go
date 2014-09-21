package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/resource/fakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("POST /checks", func() {
	var input builds.Input
	var requestURL string
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

		requestURL = server.URL + "/checks"

		requestBody = inputPayload(input)
	})

	JustBeforeEach(func() {
		var err error

		response, err = client.Post(
			requestURL,
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

		Context("and checking succeeds", func() {
			var (
				version1 builds.Version
				version2 builds.Version

				returnedVersions []builds.Version
			)

			BeforeEach(func() {
				version1 = builds.Version{"ref": "a"}
				version2 = builds.Version{"ref": "b"}

				resource.CheckReturns([]builds.Version{version1, version2}, nil)
			})

			JustBeforeEach(func() {
				err := json.NewDecoder(response.Body).Decode(&returnedVersions)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("returns 200", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusOK))
			})

			It("checks for new versions of the given input", func() {
				Ω(resource.CheckCallCount()).Should(Equal(1))
				Ω(resource.CheckArgsForCall(0)).Should(Equal(input))
			})

			It("returns the detected versions", func() {
				Ω(returnedVersions).Should(Equal([]builds.Version{version1, version2}))
			})

			It("releases the resource", func() {
				Ω(tracker.ReleaseCallCount()).Should(Equal(1))
				Ω(tracker.ReleaseArgsForCall(0)).Should(Equal(resource))
			})
		})

		Context("and checking fails", func() {
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
