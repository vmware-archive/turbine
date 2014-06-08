package api_test

import (
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/router"
	"github.com/winston-ci/prole/api"
	"github.com/winston-ci/prole/resource/fakes"
	"github.com/winston-ci/prole/routes"
	"github.com/winston-ci/prole/scheduler/fakescheduler"
)

var scheduler *fakescheduler.FakeScheduler
var tracker *fakes.FakeTracker
var drain chan struct{}

var server *httptest.Server
var client *http.Client

var _ = BeforeEach(func() {
	scheduler = fakescheduler.New()
	tracker = new(fakes.FakeTracker)
	drain = make(chan struct{})

	proleEndpoint := router.NewRequestGenerator("http://some-prole", routes.Routes)

	handler, err := api.New(scheduler, tracker, proleEndpoint, drain)
	Ω(err).ShouldNot(HaveOccurred())

	server = httptest.NewServer(handler)
	client = &http.Client{
		Transport: &http.Transport{},
	}
})

func TestApi(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "API Suite")
}

func NullLogger() *log.Logger {
	return log.New(ioutil.Discard, "", 0)
}
