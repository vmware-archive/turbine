package api_test

import (
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("DELETE /builds/:guid", func() {
	var response *http.Response

	JustBeforeEach(func() {
		var err error

		request, err := http.NewRequest("DELETE", server.URL+"/builds/some-build-guid", nil)
		Ω(err).ShouldNot(HaveOccurred())

		response, err = client.Do(request)
		Ω(err).ShouldNot(HaveOccurred())
	})

	It("deletes the build from the scheduler", func() {
		Ω(scheduler.DeleteCallCount()).Should(Equal(1))
		Ω(scheduler.DeleteArgsForCall(0)).Should(Equal("some-build-guid"))
	})

	It("returns 204", func() {
		Ω(response.StatusCode).Should(Equal(http.StatusNoContent))
	})
})
