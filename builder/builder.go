package builder

import (
	"fmt"
	"io"

	"code.google.com/p/go.net/websocket"
	"github.com/cloudfoundry-incubator/garden/warden"

	"github.com/winston-ci/prole/api/builds"
)

type Builder interface {
	Build(builds.Build) (started <-chan builds.Build, finished <-chan bool, errored <-chan error)
}

type SourceFetcher interface {
	Fetch(input builds.Input) (config builds.Config, source builds.Source, tarStream io.Reader, err error)
}

type ImageFetcher interface {
	Fetch(name string) (id string, err error)
}

type builder struct {
	sourceFetcher SourceFetcher
	wardenClient  warden.Client
}

func NewBuilder(
	sourceFetcher SourceFetcher,
	wardenClient warden.Client,
) Builder {
	return &builder{
		sourceFetcher: sourceFetcher,
		wardenClient:  wardenClient,
	}
}

type nullSink struct{}

func (nullSink) Write(data []byte) (int, error) { return len(data), nil }
func (nullSink) Close() error                   { return nil }

func (builder *builder) Build(build builds.Build) (<-chan builds.Build, <-chan bool, <-chan error) {
	started := make(chan builds.Build, 1)
	finished := make(chan bool, 1)
	errored := make(chan error, 1)

	go builder.build(build, started, finished, errored)

	return started, finished, errored
}

func (builder *builder) build(build builds.Build, started chan<- builds.Build, finished chan<- bool, errored chan<- error) {
	logs, err := builder.logsFor(build.LogsURL)
	if err != nil {
		errored <- err
		return
	}

	defer logs.Close()

	resources := map[string]io.Reader{}

	for i, input := range build.Inputs {
		buildConfig, source, tarStream, err := builder.sourceFetcher.Fetch(input)
		if err != nil {
			errored <- err
			return
		}

		build.Inputs[i].Source = source

		if input.ConfigPath != "" {
			build.Config = buildConfig
		}

		resources[input.DestinationPath] = tarStream
	}

	started <- build

	container, err := builder.createBuildContainer(build.Config, logs)
	if err != nil {
		errored <- err
		return
	}

	err = builder.streamInResources(container, resources)
	if err != nil {
		errored <- err
		return
	}

	stream, err := builder.runBuild(container, build.Privileged, build.Config, logs)
	if err != nil {
		errored <- err
		return
	}

	succeeded, err := builder.waitForRunToEnd(stream, logs)
	if err != nil {
		errored <- err
		return
	}

	finished <- succeeded
}

func (builder *builder) logsFor(logURL string) (io.WriteCloser, error) {
	if logURL == "" {
		return nullSink{}, nil
	}

	conn, err := websocket.Dial(logURL, "", "http://0.0.0.0")
	if err != nil {
		return nil, err
	}

	conn.PayloadType = websocket.BinaryFrame

	return conn, nil
}

func (builder *builder) createBuildContainer(
	buildConfig builds.Config,
	logs io.Writer,
) (warden.Container, error) {
	fmt.Fprintf(logs, "creating container from %s...\n", buildConfig.Image)

	containerSpec := warden.ContainerSpec{
		RootFSPath: "image:" + buildConfig.Image,
	}

	return builder.wardenClient.Create(containerSpec)
}

func (builder *builder) streamInResources(
	container warden.Container,
	resources map[string]io.Reader,
) error {
	for destination, streamOut := range resources {
		streamIn, err := container.StreamIn("/tmp/build/src/" + destination)
		if err != nil {
			return err
		}

		_, err = io.Copy(streamIn, streamOut)
		if err != nil {
			return err
		}

		err = streamIn.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func (builder *builder) runBuild(
	container warden.Container,
	privileged bool,
	buildConfig builds.Config,
	logs io.Writer,
) (<-chan warden.ProcessStream, error) {
	fmt.Fprintf(logs, "starting...\n")

	env := make([]warden.EnvironmentVariable, len(buildConfig.Env))
	for i, e := range buildConfig.Env {
		env[i] = warden.EnvironmentVariable{Key: e[0], Value: e[1]}
	}

	processSpec := warden.ProcessSpec{
		Privileged: privileged,

		Script: "cd /tmp/build/src\n" + buildConfig.Script,

		EnvironmentVariables: env,
	}

	_, stream, err := container.Run(processSpec)

	return stream, err
}

func (builder *builder) waitForRunToEnd(
	stream <-chan warden.ProcessStream,
	logs io.Writer,
) (bool, error) {
	for chunk := range stream {
		if chunk.ExitStatus != nil {
			return *chunk.ExitStatus == 0, nil
		}

		logs.Write(chunk.Data)
	}

	return false, fmt.Errorf("output stream interrupted")
}
