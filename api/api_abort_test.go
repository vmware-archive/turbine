package api_test

import (
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("POST /builds/:guid/abort", func() {
	var response *http.Response

	JustBeforeEach(func() {
		var err error

		response, err = client.Post(
			server.URL+"/builds/some-build-guid/abort",
			"application/json",
			nil,
		)
		Ω(err).ShouldNot(HaveOccurred())
	})

	It("returns 200", func() {
		Ω(response.StatusCode).Should(Equal(http.StatusOK))
	})

	It("aborts the build via the scheduler", func() {
		Ω(scheduler.AbortCallCount()).Should(Equal(1))
		Ω(scheduler.AbortArgsForCall(0)).Should(Equal("some-build-guid"))
	})
})
