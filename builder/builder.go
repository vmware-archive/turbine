package builder

import (
	"errors"
	"fmt"
	"io"

	"github.com/cloudfoundry-incubator/garden/warden"

	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/logwriter"
	"github.com/concourse/turbine/resource"
)

var ErrAborted = errors.New("build aborted")

type Builder interface {
	Start(builds.Build, <-chan struct{}) (started <-chan RunningBuild, errored <-chan error)
	Attach(RunningBuild, <-chan struct{}) (finished <-chan SucceededBuild, failed <-chan error, errored <-chan error)
	Complete(SucceededBuild, <-chan struct{}) (finished <-chan builds.Build, errored <-chan error)
}

type RunningBuild struct {
	Build builds.Build

	ContainerHandle string
	Container       warden.Container

	ProcessID     uint32
	ProcessStream <-chan warden.ProcessStream

	LogStream io.WriteCloser
}

type SucceededBuild struct {
	Build builds.Build

	ContainerHandle string
	Container       warden.Container

	LogStream io.WriteCloser
}

type builder struct {
	tracker      resource.Tracker
	wardenClient warden.Client
}

func NewBuilder(
	tracker resource.Tracker,
	wardenClient warden.Client,
) Builder {
	return &builder{
		tracker:      tracker,
		wardenClient: wardenClient,
	}
}

type nullSink struct{}

func (nullSink) Write(data []byte) (int, error) { return len(data), nil }
func (nullSink) Close() error                   { return nil }

func (builder *builder) Start(build builds.Build, abort <-chan struct{}) (<-chan RunningBuild, <-chan error) {
	started := make(chan RunningBuild, 1)
	errored := make(chan error, 1)

	go builder.start(build, abort, started, errored)

	return started, errored
}

func (builder *builder) Attach(running RunningBuild, abort <-chan struct{}) (<-chan SucceededBuild, <-chan error, <-chan error) {
	succeeded := make(chan SucceededBuild, 1)
	failed := make(chan error, 1)
	errored := make(chan error, 1)

	go builder.attach(running, abort, succeeded, failed, errored)

	return succeeded, failed, errored
}

func (builder *builder) Complete(succeeded SucceededBuild, abort <-chan struct{}) (<-chan builds.Build, <-chan error) {
	finished := make(chan builds.Build, 1)
	errored := make(chan error, 1)

	go builder.complete(succeeded, abort, finished, errored)

	return finished, errored
}

func (builder *builder) start(build builds.Build, abort <-chan struct{}, started chan<- RunningBuild, errored chan<- error) {
	logs := builder.logsFor(build.LogsURL)

	resources := map[string]io.Reader{}

	for i, input := range build.Inputs {
		resource, err := builder.tracker.Init(input.Type, logs, abort)
		if err != nil {
			errored <- err
			logs.Close()
			return
		}

		defer builder.tracker.Release(resource)

		tarStream, computedInput, buildConfig, err := resource.In(input)
		if err != nil {
			errored <- err
			logs.Close()
			return
		}

		build.Inputs[i] = computedInput

		build.Config = build.Config.Merge(buildConfig)

		resources[input.Name] = tarStream
	}

	container, err := builder.createBuildContainer(build.Config, logs)
	if err != nil {
		errored <- err
		logs.Close()
		return
	}

	err = builder.streamInResources(container, resources, build.Config.Paths)
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
		Build: build,

		ContainerHandle: container.Handle(),
		Container:       container,

		ProcessID:     pid,
		ProcessStream: stream,

		LogStream: logs,
	}
}

func (builder *builder) attach(running RunningBuild, abort <-chan struct{}, succeeded chan<- SucceededBuild, failed chan<- error, errored chan<- error) {
	if running.LogStream == nil {
		running.LogStream = builder.logsFor(running.Build.LogsURL)
	}

	if running.Container == nil {
		container, err := builder.wardenClient.Lookup(running.ContainerHandle)
		if err != nil {
			running.LogStream.Close()
			errored <- err
			return
		}

		running.Container = container
	}

	if running.ProcessStream == nil {
		stream, err := running.Container.Attach(running.ProcessID)
		if err != nil {
			running.LogStream.Close()
			errored <- err
			return
		}

		running.ProcessStream = stream
	}

	status, err := builder.waitForRunToEnd(running, abort)
	if err != nil {
		running.LogStream.Close()
		errored <- err
		return
	}

	if status != 0 {
		failed <- fmt.Errorf("exit status %d", status)
		return
	}

	succeeded <- SucceededBuild{
		Build:     running.Build,
		Container: running.Container,

		LogStream: running.LogStream,
	}
}

func (builder *builder) complete(succeeded SucceededBuild, abort <-chan struct{}, finished chan<- builds.Build, errored chan<- error) {
	if succeeded.LogStream == nil {
		succeeded.LogStream = builder.logsFor(succeeded.Build.LogsURL)
	}

	defer succeeded.LogStream.Close()

	outputs, err := builder.performOutputs(succeeded.Container, succeeded.Build, succeeded.LogStream, abort)
	if err != nil {
		errored <- err
		return
	}

	succeeded.Build.Outputs = outputs

	finished <- succeeded.Build
}

func (builder *builder) logsFor(logURL string) io.WriteCloser {
	if logURL == "" {
		return nullSink{}
	}

	return logwriter.NewWriter(logURL)
}

func (builder *builder) createBuildContainer(
	buildConfig builds.Config,
	logs io.Writer,
) (warden.Container, error) {
	fmt.Fprintf(logs, "creating container from %s...\n", buildConfig.Image)

	containerSpec := warden.ContainerSpec{
		RootFSPath: buildConfig.Image,
	}

	return builder.wardenClient.Create(containerSpec)
}

func (builder *builder) streamInResources(
	container warden.Container,
	resources map[string]io.Reader,
	paths map[string]string,
) error {
	for name, streamOut := range resources {
		destination, found := paths[name]
		if !found {
			destination = name
		}

		err := container.StreamIn("/tmp/build/src/"+destination, streamOut)
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

	env := []warden.EnvironmentVariable{}
	for n, v := range buildConfig.Params {
		env = append(env, warden.EnvironmentVariable{Key: n, Value: v})
	}

	processSpec := warden.ProcessSpec{
		Privileged: privileged,

		Path: buildConfig.Run.Path,
		Args: buildConfig.Run.Args,

		Dir: "/tmp/build/src",

		EnvironmentVariables: env,
	}

	return container.Run(processSpec)
}

func (builder *builder) waitForRunToEnd(
	running RunningBuild,
	abort <-chan struct{},
) (uint32, error) {
	for {
		select {
		case chunk := <-running.ProcessStream:
			if chunk.ExitStatus != nil {
				return *chunk.ExitStatus, nil
			}

			running.LogStream.Write(chunk.Data)
		case <-abort:
			running.Container.Stop(false)
			return 0, ErrAborted
		}
	}

	return 0, fmt.Errorf("output stream interrupted")
}

func (builder *builder) performOutputs(
	container warden.Container,
	build builds.Build,
	logs io.Writer,
	abort <-chan struct{},
) ([]builds.Output, error) {
	allOutputs := map[string]builds.Output{}

	// Implicit outputs
	for _, input := range build.Inputs {
		allOutputs[input.Name] = builds.Output{
			Name:    input.Name,
			Type:    input.Type,
			Version: input.Version,
		}
	}

	if len(build.Outputs) > 0 {
		errs := make(chan error, len(build.Outputs))
		results := make(chan builds.Output, len(build.Outputs))

		for _, output := range build.Outputs {
			go func(output builds.Output) {
				streamOut, err := container.StreamOut("/tmp/build/src/")
				if err != nil {
					errs <- err
					return
				}

				resource, err := builder.tracker.Init(output.Type, logs, abort)
				if err != nil {
					errs <- err
					return
				}

				defer builder.tracker.Release(resource)

				computedOutput, err := resource.Out(streamOut, output)

				errs <- err
				results <- computedOutput
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
