package resource

import (
	"io"

	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/concourse/turbine/api/builds"
)

type Resource interface {
	In(builds.Input) (io.Reader, builds.Input, builds.Config, error)
	Out(io.Reader, builds.Output) (builds.Output, error)
	Check(builds.Input) ([]builds.Version, error)
}

const ResourcesDir = "/tmp/build/src"

type resource struct {
	container garden_api.Container
	logs      io.Writer
	abort     <-chan struct{}
}

func NewResource(
	container garden_api.Container,
	logs io.Writer,
	abort <-chan struct{},
) Resource {
	return &resource{
		container: container,
		logs:      logs,
		abort:     abort,
	}
}
