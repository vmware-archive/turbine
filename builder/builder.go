package builder

import (
	"errors"
	"fmt"
	"io"

	"code.google.com/p/go.net/websocket"
	"github.com/cloudfoundry-incubator/garden/warden"

	"github.com/winston-ci/prole/api/builds"
)

type Builder interface {
	Build(builds.Build) (started <-chan RunningBuild, errored <-chan error)
	Attach(RunningBuild) (finished <-chan builds.Build, failed <-chan error, errored <-chan error)
}

type SourceFetcher interface {
	Fetch(input builds.Input) (config builds.Config, source builds.Source, tarStream io.Reader, err error)
}

type Outputter interface {
	PerformOutput(builds.Output, io.Reader) (builds.Source, error)
}

type RunningBuild struct {
	Build     builds.Build
	Container warden.Container

	ProcessID     uint32
	ProcessStream <-chan warden.ProcessStream

	LogStream io.WriteCloser
}

type builder struct {
	sourceFetcher SourceFetcher
	outputter     Outputter
	wardenClient  warden.Client
}

func NewBuilder(
	sourceFetcher SourceFetcher,
	outputter Outputter,
	wardenClient warden.Client,
) Builder {
	return &builder{
		sourceFetcher: sourceFetcher,
		outputter:     outputter,
		wardenClient:  wardenClient,
	}
}

type nullSink struct{}

func (nullSink) Write(data []byte) (int, error) { return len(data), nil }
func (nullSink) Close() error                   { return nil }

func (builder *builder) Build(build builds.Build) (<-chan RunningBuild, <-chan error) {
	started := make(chan RunningBuild, 1)
	errored := make(chan error, 1)

	go builder.build(build, started, errored)

	return started, errored
}

func (builder *builder) build(build builds.Build, started chan<- RunningBuild, errored chan<- error) {
	logs, err := builder.logsFor(build.LogsURL)
	if err != nil {
		errored <- err
		return
	}

	resources := map[string]io.Reader{}

	for i, input := range build.Inputs {
		buildConfig, source, tarStream, err := builder.sourceFetcher.Fetch(input)
		if err != nil {
			errored <- err
			logs.Close()
			return
		}

		build.Inputs[i].Source = source

		if input.ConfigPath != "" {
			build.Config = buildConfig
		}

		resources[input.DestinationPath] = tarStream
	}

	container, err := builder.createBuildContainer(build.Config, logs)
	if err != nil {
		errored <- err
		logs.Close()
		return
	}

	err = builder.streamInResources(container, resources)
	if err != nil {
		errored <- err
		logs.Close()
		return
	}

	pid, stream, err := builder.runBuild(container, build.Privileged, build.Config, logs)
	if err != nil {
		errored <- err
		logs.Close()
		return
	}

	started <- RunningBuild{
		Build:     build,
		Container: container,

		ProcessID:     pid,
		ProcessStream: stream,

		LogStream: logs,
	}
}

func (builder *builder) Attach(running RunningBuild) (<-chan builds.Build, <-chan error, <-chan error) {
	failed := make(chan error, 1)
	errored := make(chan error, 1)
	finished := make(chan builds.Build, 1)

	go builder.attach(running, finished, failed, errored)

	return finished, failed, errored
}

func (builder *builder) attach(running RunningBuild, finished chan<- builds.Build, failed chan<- error, errored chan<- error) {
	var logs io.WriteCloser

	if running.LogStream != nil {
		logs = running.LogStream
	} else {
		var err error

		logs, err = builder.logsFor(running.Build.LogsURL)
		if err != nil {
			errored <- err
			return
		}
	}

	defer logs.Close()

	var stream <-chan warden.ProcessStream

	if running.ProcessStream != nil {
		stream = running.ProcessStream
	} else {
		var err error

		stream, err = running.Container.Attach(running.ProcessID)
		if err != nil {
			errored <- err
			return
		}
	}

	succeeded, err := builder.waitForRunToEnd(stream, logs)
	if err != nil {
		errored <- err
		return
	}

	if !succeeded {
		failed <- errors.New("nonzero exit status") // TODO
		return
	}

	outputs, err := builder.performOutputs(running.Container, running.Build)
	if err != nil {
		errored <- err
		return
	}

	running.Build.Outputs = outputs

	finished <- running.Build
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
		RootFSPath: "docker:///" + buildConfig.Image,
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
) (uint32, <-chan warden.ProcessStream, error) {
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

	return container.Run(processSpec)
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

func (builder *builder) performOutputs(container warden.Container, build builds.Build) ([]builds.Output, error) {
	allOutputs := map[string]builds.Output{}
	for _, input := range build.Inputs {
		allOutputs[input.Name] = builds.Output{
			Name:   input.Name,
			Type:   input.Type,
			Source: input.Source,
		}
	}

	if len(build.Outputs) > 0 {
		errs := make(chan error, len(build.Outputs))
		results := make(chan builds.Output, len(build.Outputs))

		for _, output := range build.Outputs {
			go func(output builds.Output) {
				source, err := builder.performOutput(container, output)

				errs <- err

				if err == nil {
					output.Source = source
					results <- output
				}
			}(output)
		}

		var outputErr error
		for i := 0; i < len(build.Outputs); i++ {
			err := <-errs
			if err != nil {
				outputErr = err
			}
		}

		if outputErr != nil {
			return nil, outputErr
		}

		for i := 0; i < len(build.Outputs); i++ {
			output := <-results
			allOutputs[output.Name] = output
		}
	}

	outputs := []builds.Output{}
	for _, output := range allOutputs {
		outputs = append(outputs, output)
	}

	return outputs, nil
}

func (builder *builder) performOutput(container warden.Container, output builds.Output) (builds.Source, error) {
	streamOut, err := container.StreamOut("/tmp/build/src/")
	if err != nil {
		return nil, err
	}

	return builder.outputter.PerformOutput(output, streamOut)
}
