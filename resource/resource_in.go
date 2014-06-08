package resource

import (
	"archive/tar"
	"io"
	"path"

	"github.com/fraenkel/candiedyaml"
	"github.com/winston-ci/prole/api/builds"
)

// Request payload from sourcefetcher to /tmp/resource/in script
type inRequest struct {
	Version builds.Version `json:"version,omitempty"`
	Source  builds.Source  `json:"source"`
}

// Response payload from /tmp/resource/in script to sourcefetcher
type inResponse struct {
	// Version is returned because request payload
	// may not contain Version to fetch relying on
	// 'in' script to fetch latest version.
	Version builds.Version `json:"version"`

	Metadata []builds.MetadataField `json:"metadata,omitempty"`
}

func (resource *resource) In(input builds.Input) (io.Reader, builds.Input, builds.Config, error) {
	var resp inResponse

	err := resource.runScript(
		"/tmp/resource/in /tmp/resource-destination",
		inRequest{input.Version, input.Source},
		&resp,
	)
	if err != nil {
		return nil, builds.Input{}, builds.Config{}, err
	}

	buildConfig, err := resource.extractConfig(input.ConfigPath)
	if err != nil {
		return nil, builds.Input{}, builds.Config{}, err
	}

	input.Version = resp.Version
	input.Metadata = resp.Metadata

	outStream, err := resource.container.StreamOut("/tmp/resource-destination/")
	if err != nil {
		return nil, builds.Input{}, builds.Config{}, err
	}

	return outStream, input, buildConfig, nil
}

func (resource *resource) extractConfig(configPath string) (builds.Config, error) {
	var buildConfig builds.Config

	if configPath == "" {
		return buildConfig, nil
	}

	configStream, err := resource.container.StreamOut(path.Join("/tmp/resource-destination", configPath))
	if err != nil {
		return buildConfig, err
	}

	reader := tar.NewReader(configStream)

	_, err = reader.Next()
	if err != nil {
		return buildConfig, err
	}

	var configFile ConfigFile

	err = candiedyaml.NewDecoder(reader).Decode(&configFile)
	if err != nil {
		return buildConfig, err
	}

	return configFile.AsBuildConfig()
}
