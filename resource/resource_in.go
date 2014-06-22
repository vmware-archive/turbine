package resource

import (
	"archive/tar"
	"io"
	"path"

	"github.com/concourse/turbine/api/builds"
	"github.com/fraenkel/candiedyaml"
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
		"/tmp/resource/in /tmp/build/src/"+input.DestinationPath,
		inRequest{input.Version, input.Source},
		&resp,
	)
	if err != nil {
		return nil, builds.Input{}, builds.Config{}, err
	}

	buildConfig, err := resource.extractConfig(input)
	if err != nil {
		return nil, builds.Input{}, builds.Config{}, err
	}

	input.Version = resp.Version
	input.Metadata = resp.Metadata

	outStream, err := resource.container.StreamOut(path.Join("/tmp/build/src", input.DestinationPath) + "/")
	if err != nil {
		return nil, builds.Input{}, builds.Config{}, err
	}

	return outStream, input, buildConfig, nil
}

func (resource *resource) extractConfig(input builds.Input) (builds.Config, error) {
	var buildConfig builds.Config

	if input.ConfigPath == "" {
		return buildConfig, nil
	}

	config := path.Join("/tmp/build/src", input.DestinationPath, input.ConfigPath)

	configStream, err := resource.container.StreamOut(config)
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

	return builds.Config{
		Image:  configFile.Image,
		Script: configFile.Script,
		Env:    configFile.Env,
	}, nil
}
