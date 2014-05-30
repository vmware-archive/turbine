package outputter

import (
	"errors"
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

// Request payload fromoutputter to /tmp/resource/out script
type outRequest struct {
	Params builds.Params `json:"params"`
}

// Response payload from /tmp/resource/out script to outputter
type outResponse struct {
	Version  builds.Version         `json:"version"`
	Metadata []builds.MetadataField `json:"metadata"`
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

func (outputter *Outputter) PerformOutput(
	output builds.Output,
	sourceStream io.Reader,
	logs io.Writer,
) (builds.Version, []builds.MetadataField, error) {
	resourceType, found := outputter.resourceTypes.Lookup(output.Type)
	if !found {
		return nil, nil, ErrUnknownSourceType
	}

	container, err := outputter.wardenClient.Create(warden.ContainerSpec{
		RootFSPath: "docker:///" + resourceType.Image,
	})
	if err != nil {
		return nil, nil, err
	}

	err = outputter.streamInSource(container, sourceStream)
	if err != nil {
		return nil, nil, err
	}

	var resp outResponse

	err = scriptrunner.Run(
		container,
		"/tmp/resource/out "+path.Join("/tmp/build/src", output.SourcePath),
		logs,
		outRequest{output.Params},
		&resp,
	)
	if err != nil {
		return nil, nil, err
	}

	return resp.Version, resp.Metadata, nil
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
