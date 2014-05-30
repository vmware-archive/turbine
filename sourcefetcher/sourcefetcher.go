package sourcefetcher

import (
	"archive/tar"
	"errors"
	"io"
	"path"

	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/fraenkel/candiedyaml"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/config"
	"github.com/winston-ci/prole/scriptrunner"
)

var ErrUnknownSourceType = errors.New("unknown source type")

type SourceFetcher struct {
	resourceTypes config.ResourceTypes
	wardenClient  warden.Client
}

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

func NewSourceFetcher(
	resourceTypes config.ResourceTypes,
	wardenClient warden.Client,
) *SourceFetcher {
	return &SourceFetcher{
		resourceTypes: resourceTypes,
		wardenClient:  wardenClient,
	}
}

func (fetcher *SourceFetcher) Fetch(input builds.Input, logs io.Writer) (builds.Config, builds.Version, []builds.MetadataField, io.Reader, error) {
	var buildConfig builds.Config

	resourceType, found := fetcher.resourceTypes.Lookup(input.Type)
	if !found {
		return buildConfig, nil, nil, nil, ErrUnknownSourceType
	}

	container, err := fetcher.wardenClient.Create(warden.ContainerSpec{
		RootFSPath: "docker:///" + resourceType.Image,
	})
	if err != nil {
		return buildConfig, nil, nil, nil, err
	}

	var resp inResponse

	err = scriptrunner.Run(
		container,
		"/tmp/resource/in /tmp/resource-destination",
		logs,
		inRequest{input.Version, input.Source},
		&resp,
	)
	if err != nil {
		return buildConfig, nil, nil, nil, err
	}

	buildConfig, err = fetcher.extractConfig(container, input.ConfigPath)
	if err != nil {
		return buildConfig, nil, nil, nil, err
	}

	outStream, err := container.StreamOut("/tmp/resource-destination/")

	return buildConfig, resp.Version, resp.Metadata, outStream, err
}

func (fetcher *SourceFetcher) extractConfig(container warden.Container, configPath string) (builds.Config, error) {
	var buildConfig builds.Config

	if configPath == "" {
		return buildConfig, nil
	}

	configStream, err := container.StreamOut(path.Join("/tmp/resource-destination", configPath))
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
