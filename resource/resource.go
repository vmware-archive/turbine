package resource

import (
	"io"

	garden "github.com/cloudfoundry-incubator/garden/api"
	"github.com/concourse/turbine"
)

type Resource interface {
	In(turbine.Input) (io.Reader, turbine.Input, turbine.Config, error)
	Out(io.Reader, turbine.Output) (turbine.Output, error)
	Check(turbine.Input) ([]turbine.Version, error)
}

const ResourcesDir = "/tmp/build/src"

type resource struct {
	container garden.Container
	logs      io.Writer
	abort     <-chan struct{}
}

func NewResource(
	container garden.Container,
	logs io.Writer,
	abort <-chan struct{},
) Resource {
	return &resource{
		container: container,
		logs:      logs,
		abort:     abort,
	}
}
