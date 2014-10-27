package resource

import (
	"archive/tar"
	"fmt"
	"io"
	"path"

	"github.com/cloudfoundry-incubator/candiedyaml"

	"github.com/concourse/turbine"
)

// Request payload from sourcefetcher to /opt/resource/in script
type inRequest struct {
	Source  turbine.Source  `json:"source"`
	Params  turbine.Params  `json:"params,omitempty"`
	Version turbine.Version `json:"version,omitempty"`
}

// Response payload from /opt/resource/in script to sourcefetcher
type inResponse struct {
	// Version is returned because request payload
	// may not contain Version to fetch relying on
	// 'in' script to fetch latest version.
	Version turbine.Version `json:"version"`

	Metadata []turbine.MetadataField `json:"metadata,omitempty"`
}

func (resource *resource) In(input turbine.Input) (io.Reader, turbine.Input, turbine.Config, error) {
	var resp inResponse

	err := resource.runScript(
		"/opt/resource/in",
		[]string{path.Join(ResourcesDir, input.Name)},
		inRequest{input.Source, input.Params, input.Version},
		&resp,
	)
	if err != nil {
		return nil, turbine.Input{}, turbine.Config{}, err
	}

	buildConfig, err := resource.extractConfig(input)
	if err != nil {
		return nil, turbine.Input{}, turbine.Config{}, err
	}

	input.Version = resp.Version
	input.Metadata = resp.Metadata

	outStream, err := resource.container.StreamOut(path.Join(ResourcesDir, input.Name) + "/")
	if err != nil {
		return nil, turbine.Input{}, turbine.Config{}, err
	}

	return outStream, input, buildConfig, nil
}

func (resource *resource) extractConfig(input turbine.Input) (turbine.Config, error) {
	if input.ConfigPath == "" {
		return turbine.Config{}, nil
	}

	configPath := path.Join(ResourcesDir, input.Name, input.ConfigPath)

	configStream, err := resource.container.StreamOut(configPath)
	if err != nil {
		return turbine.Config{}, err
	}

	reader := tar.NewReader(configStream)

	_, err = reader.Next()
	if err != nil {
		if err == io.EOF {
			return turbine.Config{}, fmt.Errorf("could not find build config '%s'", input.ConfigPath)
		}

		return turbine.Config{}, err
	}

	var buildConfig turbine.Config

	err = candiedyaml.NewDecoder(reader).Decode(&buildConfig)
	if err != nil {
		return turbine.Config{}, fmt.Errorf("invalid build config '%s': %s", input.ConfigPath, err)
	}

	return buildConfig, nil
}
