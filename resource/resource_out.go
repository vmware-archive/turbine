package resource

import (
	"io"

	"github.com/concourse/turbine"
)

// Request payload from resource to /opt/resource/out script
type outRequest struct {
	Source  turbine.Source  `json:"source"`
	Params  turbine.Params  `json:"params,omitempty"`
	Version turbine.Version `json:"version,omitempty"`
}

// Response payload from /opt/resource/out script to resource
type outResponse struct {
	Version  turbine.Version         `json:"version"`
	Metadata []turbine.MetadataField `json:"metadata"`
}

func (resource *resource) Out(sourceStream io.Reader, output turbine.Output) (turbine.Output, error) {
	err := resource.container.StreamIn(ResourcesDir, sourceStream)
	if err != nil {
		return turbine.Output{}, err
	}

	var resp outResponse

	err = resource.runScript(
		"/opt/resource/out",
		[]string{ResourcesDir},
		outRequest{
			Params:  output.Params,
			Source:  output.Source,
			Version: output.Version,
		},
		&resp,
	)
	if err != nil {
		return turbine.Output{}, err
	}

	output.Version = resp.Version
	output.Metadata = resp.Metadata

	return output, nil
}
