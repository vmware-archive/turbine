package builder

import (
	"github.com/cloudfoundry-incubator/garden/backend"
	"github.com/cloudfoundry-incubator/gordon"

	"github.com/room101-ci/agent/api/builds"
)

type SourceFetcher interface {
	Fetch(source builds.BuildSource) (directory string, err error)
}

type ImageFetcher interface {
	Fetch(name string) (id string, err error)
}

type Builder struct {
	sourceFetcher SourceFetcher
	imageFetcher  ImageFetcher
	wardenClient  gordon.Client
}

func NewBuilder(
	sourceFetcher SourceFetcher,
	imageFetcher ImageFetcher,
	wardenClient gordon.Client,
) *Builder {
	return &Builder{
		sourceFetcher: sourceFetcher,
		imageFetcher:  imageFetcher,
		wardenClient:  wardenClient,
	}
}

func (builder *Builder) Build(build *builds.Build) (bool, error) {
	imageID, err := builder.imageFetcher.Fetch(build.Image)
	if err != nil {
		return false, err
	}

	_, err = builder.sourceFetcher.Fetch(build.Source)
	if err != nil {
		return false, err
	}

	_, err = builder.wardenClient.Create(backend.ContainerSpec{
		RootFSPath: "image:" + imageID,
	})
	if err != nil {
		return false, err
	}

	return true, nil
}
