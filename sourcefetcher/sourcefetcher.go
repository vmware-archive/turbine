package sourcefetcher

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/fraenkel/candiedyaml"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/config"
)

var ErrUnknownSourceType = errors.New("unknown source type")

type InputConfig struct {
	Source    builds.BuildSource `json:"source"`
	ConfigTag []byte             `json:"config"`
	Payload   []byte             `json:"payload"`
}

type ErrResourceFetchFailed struct {
	Stdout     []byte
	Stderr     []byte
	ExitStatus uint32
}

func (err ErrResourceFetchFailed) Error() string {
	return fmt.Sprintf(
		"resource fetching failed: exit status %d\n\nstdout:\n\n%s\n\nstderr:%s",
		err.ExitStatus,
		err.Stdout,
		err.Stderr,
	)
}

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

func (fetcher *SourceFetcher) Fetch(source builds.BuildSource, payload []byte) (builds.BuildConfig, io.Reader, error) {
	var buildConfig builds.BuildConfig

	resourceType, found := fetcher.resourceTypes.Lookup(source.Type)
	if !found {
		return buildConfig, nil, ErrUnknownSourceType
	}

	container, err := fetcher.wardenClient.Create(warden.ContainerSpec{
		RootFSPath: "image:" + resourceType.Image,
	})
	if err != nil {
		return buildConfig, nil, err
	}

	err = fetcher.injectInputConfig(container, payload)
	if err != nil {
		return buildConfig, nil, err
	}

	_, stream, err := container.Run(warden.ProcessSpec{
		Script: "/tmp/resource/in /tmp/resource-destination < /tmp/resource-artifacts/input.json",
	})
	if err != nil {
		return buildConfig, nil, err
	}

	err = fetcher.waitForRunToEnd(stream)
	if err != nil {
		return buildConfig, nil, err
	}

	buildConfig, err = fetcher.extractBuildConfig(container, source.ConfigPath)
	if err != nil {
		return buildConfig, nil, err
	}

	outStream, err := container.StreamOut("/tmp/resource-destination/")

	return buildConfig, outStream, err
}

func (fetcher *SourceFetcher) injectInputConfig(container warden.Container, payload []byte) error {
	streamIn, err := container.StreamIn("/tmp/resource-artifacts/")
	if err != nil {
		return err
	}

	tarWriter := tar.NewWriter(streamIn)

	err = tarWriter.WriteHeader(&tar.Header{
		Name: "./input.json",
		Mode: 0644,
		Size: int64(len(payload)),
	})
	if err != nil {
		return err
	}

	_, err = tarWriter.Write(payload)
	if err != nil {
		return err
	}

	err = tarWriter.Close()
	if err != nil {
		return err
	}

	err = streamIn.Close()
	if err != nil {
		return err
	}

	return nil
}

func (fetcher *SourceFetcher) waitForRunToEnd(stream <-chan warden.ProcessStream) error {
	stdout := []byte{}
	stderr := []byte{}

	for chunk := range stream {
		if chunk.ExitStatus != nil {
			if *chunk.ExitStatus != 0 {
				return ErrResourceFetchFailed{
					Stdout:     stdout,
					Stderr:     stderr,
					ExitStatus: *chunk.ExitStatus,
				}
			}

			break
		}

		switch chunk.Source {
		case warden.ProcessStreamSourceStdout:
			stdout = append(stdout, chunk.Data...)
		case warden.ProcessStreamSourceStderr:
			stderr = append(stderr, chunk.Data...)
		}
	}

	return nil
}

func (fetcher *SourceFetcher) extractBuildConfig(container warden.Container, configPath string) (builds.BuildConfig, error) {
	var buildConfig builds.BuildConfig

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
