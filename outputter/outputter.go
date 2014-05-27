package outputter

import (
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/cloudfoundry-incubator/garden/warden"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/config"
	"github.com/winston-ci/prole/scriptrunner"
)

var ErrUnknownSourceType = errors.New("unknown source type")

type Outputter struct {
	resourceTypes config.ResourceTypes
	wardenClient  warden.Client
}

func NewOutputter(
	resourceTypes config.ResourceTypes,
	wardenClient warden.Client,
) *Outputter {
	return &Outputter{
		resourceTypes: resourceTypes,
		wardenClient:  wardenClient,
	}
}

func (outputter *Outputter) PerformOutput(output builds.Output, sourceStream io.Reader, logs io.Writer) (builds.Source, error) {
	resourceType, found := outputter.resourceTypes.Lookup(output.Type)
	if !found {
		return nil, ErrUnknownSourceType
	}

	container, err := outputter.wardenClient.Create(warden.ContainerSpec{
		RootFSPath: "docker:///" + resourceType.Image,
	})
	if err != nil {
		return nil, err
	}

	err = outputter.streamInSource(container, sourceStream)
	if err != nil {
		return nil, err
	}

	var source builds.Source

	err = scriptrunner.Run(
		container,
		fmt.Sprintf("/tmp/resource/out %s", path.Join("/tmp/build/src", output.SourcePath)),
		logs,
		output.Params,
		&source,
	)
	if err != nil {
		return nil, err
	}

	return source, nil
}

func (outputter *Outputter) streamInSource(
	container warden.Container,
	sourceStream io.Reader,
) error {
	streamIn, err := container.StreamIn("/tmp/build/src")
	if err != nil {
		return err
	}

	_, err = io.Copy(streamIn, sourceStream)
	if err != nil {
		return err
	}

	return streamIn.Close()
}
