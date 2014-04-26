package builder

import (
	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"
	"github.com/cloudfoundry-incubator/garden/warden"

	"github.com/winston-ci/prole/api/builds"
)

type SourceFetcher interface {
	Fetch(source builds.BuildSource) (directory string, err error)
}

type ImageFetcher interface {
	Fetch(name string) (id string, err error)
}

type Builder struct {
	sourceFetcher      SourceFetcher
	wardenClient       warden.Client
	logStreamerFactory log_streamer_factory.LogStreamerFactory
}

func NewBuilder(
	sourceFetcher SourceFetcher,
	wardenClient warden.Client,
	logStreamerFactory log_streamer_factory.LogStreamerFactory,
) *Builder {
	return &Builder{
		sourceFetcher:      sourceFetcher,
		wardenClient:       wardenClient,
		logStreamerFactory: logStreamerFactory,
	}
}

func (builder *Builder) Build(build *builds.Build) (bool, error) {
	logStreamer := builder.logStreamerFactory(build.LogConfig)

	fetchedSource, err := builder.sourceFetcher.Fetch(build.Source)
	if err != nil {
		return false, err
	}

	container, err := builder.wardenClient.Create(warden.ContainerSpec{
		RootFSPath: "image:" + build.Image,
	})
	if err != nil {
		return false, err
	}

	err = container.CopyIn(fetchedSource+"/", "./")
	if err != nil {
		return false, err
	}

	_, stream, err := container.Run(warden.ProcessSpec{Script: build.Script})
	if err != nil {
		return false, err
	}

	succeeded := false

	for chunk := range stream {
		if chunk.ExitStatus != nil {
			succeeded = *chunk.ExitStatus == 0
		}

		switch chunk.Source {
		case warden.ProcessStreamSourceStdout:
			logStreamer.Stdout().Write(chunk.Data)
		case warden.ProcessStreamSourceStderr:
			logStreamer.Stderr().Write(chunk.Data)
		}

		logStreamer.Flush()
	}

	return succeeded, nil
}
