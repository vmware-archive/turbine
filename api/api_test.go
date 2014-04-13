package api_test

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/room101-ci/agent/api"
)

var _ = Describe("API", func() {
	var server *httptest.Server
	var client *http.Client

	BeforeEach(func() {
		handler := api.New(NullLogger())
		server = httptest.NewServer(handler)
		client = &http.Client{}
	})

	Describe("GET /builds/:build", func() {
		It("returns 200", func() {
			response, err := client.Get(server.URL + "/builds/abc")
			Expect(err).NotTo(HaveOccurred())
			Expect(response.StatusCode).To(Equal(200))
		})
	})
})
