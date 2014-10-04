package resource_test

import (
	"testing"

	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/garden/client/fake_api_client"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	. "github.com/concourse/turbine/resource"
)

var (
	gardenClient *fake_api_client.FakeClient

	logs  *gbytes.Buffer
	abort chan struct{}

	resource Resource
)

var _ = BeforeEach(func() {
	gardenClient = fake_api_client.New()

	gardenClient.Connection.CreateReturns("some-handle", nil)

	container, err := gardenClient.Create(garden_api.ContainerSpec{})
	Ω(err).ShouldNot(HaveOccurred())

	logs = gbytes.NewBuffer()
	abort = make(chan struct{})

	resource = NewResource(container, logs, abort)
})

func TestResource(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Resource Suite")
}
