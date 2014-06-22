package resource

import (
	"io"

	"github.com/concourse/turbine/api/builds"
)

// Request payload from resource to /tmp/resource/out script
type outRequest struct {
	Params builds.Params `json:"params"`
	Source builds.Source `json:"source"`
}

// Response payload from /tmp/resource/out script to resource
type outResponse struct {
	Version  builds.Version         `json:"version"`
	Metadata []builds.MetadataField `json:"metadata"`
}

func (resource *resource) Out(sourceStream io.Reader, output builds.Output) (builds.Output, error) {
	err := resource.streamInSource(sourceStream)
	if err != nil {
		return builds.Output{}, err
	}

	var resp outResponse

	err = resource.runScript(
		"/tmp/resource/out /tmp/build/src",
		outRequest{
			Params: output.Params,
			Source: output.Source,
		},
		&resp,
	)
	if err != nil {
		return builds.Output{}, err
	}

	output.Version = resp.Version
	output.Metadata = resp.Metadata

	return output, nil
}

func (resource *resource) streamInSource(sourceStream io.Reader) error {
	streamIn, err := resource.container.StreamIn("/tmp/build/src")
	if err != nil {
		return err
	}

	_, err = io.Copy(streamIn, sourceStream)
	if err != nil {
		return err
	}

	return streamIn.Close()
}
