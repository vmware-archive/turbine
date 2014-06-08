package resource_test

import (
	"testing"

	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	. "github.com/winston-ci/prole/resource"
)

var (
	wardenClient *fake_warden_client.FakeClient

	logs  *gbytes.Buffer
	abort chan struct{}

	resource Resource
)

var _ = BeforeEach(func() {
	wardenClient = fake_warden_client.New()

	wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
		return "some-handle", nil
	}

	container, err := wardenClient.Create(warden.ContainerSpec{})
	Ω(err).ShouldNot(HaveOccurred())

	logs = gbytes.NewBuffer()
	abort = make(chan struct{})

	resource = NewResource(container, logs, abort)
})

func primedStream(payloads ...warden.ProcessStream) <-chan warden.ProcessStream {
	stream := make(chan warden.ProcessStream, len(payloads))

	for _, payload := range payloads {
		stream <- payload
	}

	close(stream)

	return stream
}

func TestResource(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Resource Suite")
}
