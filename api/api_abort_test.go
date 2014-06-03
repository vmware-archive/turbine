package api_test

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/router"

	"github.com/winston-ci/prole/api"
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

	Describe("POST /builds/:guid/abort", func() {
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
			Ω(scheduler.Aborted()).Should(Equal([]string{"some-build-guid"}))
		})
	})
})
