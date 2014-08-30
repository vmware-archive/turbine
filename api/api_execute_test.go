package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/concourse/turbine/api/builds"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("POST /builds", func() {
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
		Ω(returnedBuild.AbortURL).Should(Equal("http://some-turbine/builds/" + returnedBuild.Guid + "/abort"))
		Ω(returnedBuild.HijackURL).Should(Equal("http://some-turbine/builds/" + returnedBuild.Guid + "/hijack"))
		Ω(returnedBuild.Inputs).Should(Equal(build.Inputs))

		Ω(scheduler.StartCallCount()).Should(Equal(1))
		Ω(scheduler.StartArgsForCall(0)).Should(Equal(returnedBuild))
	})

	Context("when the callback url is malformed", func() {
		BeforeEach(func() {
			build.StatusCallback = "ß"
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
