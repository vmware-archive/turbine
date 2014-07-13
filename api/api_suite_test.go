package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/concourse/turbine/api"
	rfakes "github.com/concourse/turbine/resource/fakes"
	"github.com/concourse/turbine/routes"
	sfakes "github.com/concourse/turbine/scheduler/fakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/rata"
)

var scheduler *sfakes.FakeScheduler
var tracker *rfakes.FakeTracker
var drain chan struct{}

var server *httptest.Server
var client *http.Client

var _ = BeforeEach(func() {
	scheduler = new(sfakes.FakeScheduler)
	tracker = new(rfakes.FakeTracker)
	drain = make(chan struct{})

	turbineEndpoint := rata.NewRequestGenerator("http://some-turbine", routes.Routes)

	handler, err := api.New(lagertest.NewTestLogger("test"), scheduler, tracker, turbineEndpoint, drain)
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
