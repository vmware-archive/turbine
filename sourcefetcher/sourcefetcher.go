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

func NewSourceFetcher(
	resourceTypes config.ResourceTypes,
	wardenClient warden.Client,
) *SourceFetcher {
	return &SourceFetcher{
		resourceTypes: resourceTypes,
		wardenClient:  wardenClient,
	}
}

func (fetcher *SourceFetcher) Fetch(input builds.Input) (builds.Config, builds.Source, io.Reader, error) {
	var buildConfig builds.Config

	resourceType, found := fetcher.resourceTypes.Lookup(input.Type)
	if !found {
		return buildConfig, nil, nil, ErrUnknownSourceType
	}

	container, err := fetcher.wardenClient.Create(warden.ContainerSpec{
		RootFSPath: "docker:///" + resourceType.Image,
	})
	if err != nil {
		return buildConfig, nil, nil, err
	}

	var source builds.Source

	err = scriptrunner.Run(
		container,
		"/tmp/resource/in /tmp/resource-destination",
		nil,
		input.Source,
		&source,
	)
	if err != nil {
		return buildConfig, nil, nil, err
	}

	buildConfig, err = fetcher.extractConfig(container, input.ConfigPath)
	if err != nil {
		return buildConfig, nil, nil, err
	}

	outStream, err := container.StreamOut("/tmp/resource-destination/")

	return buildConfig, source, outStream, err
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
