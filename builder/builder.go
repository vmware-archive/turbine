package builder

import (
	"errors"
	"fmt"
	"path"
	"time"

	"github.com/cloudfoundry-incubator/garden/warden"

	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/builder/inputs"
	"github.com/concourse/turbine/builder/outputs"
	"github.com/concourse/turbine/event"
	"github.com/concourse/turbine/logwriter"
)

var ErrAborted = errors.New("build aborted")

type Builder interface {
	// begin execution of a build, fetching all inputs and spawning the process
	Start(builds.Build, event.Emitter, <-chan struct{}) (RunningBuild, error)

	// attach to a running build, forwarding output events
	//
	// this will be called again after turbine restarts
	Attach(RunningBuild, event.Emitter, <-chan struct{}) (ExitedBuild, error)

	// execute an arbitrary process in a running build
	Hijack(RunningBuild, warden.ProcessSpec, warden.ProcessIO) (warden.Process, error)

	// process an exited build's outputs
	Finish(ExitedBuild, event.Emitter, <-chan struct{}) (builds.Build, error)
}

type RunningBuild struct {
	Build builds.Build

	ContainerHandle string
	Container       warden.Container

	ProcessID uint32
	Process   warden.Process
}

type ExitedBuild struct {
	Build builds.Build

	ContainerHandle string
	Container       warden.Container

	ExitStatus int
}

type builder struct {
	wardenClient    warden.Client
	inputFetcher    inputs.Fetcher
	outputPerformer outputs.Performer
}

func NewBuilder(
	wardenClient warden.Client,
	inputFetcher inputs.Fetcher,
	outputPerformer outputs.Performer,
) Builder {
	return &builder{
		wardenClient:    wardenClient,
		inputFetcher:    inputFetcher,
		outputPerformer: outputPerformer,
	}
}

func (builder *builder) Start(build builds.Build, emitter event.Emitter, abort <-chan struct{}) (RunningBuild, error) {
	fetchedInputs, err := builder.inputFetcher.Fetch(build.Inputs, emitter, abort)
	if err != nil {
		return RunningBuild{}, builder.emitError(emitter, "failed to fetch inputs", err)
	}

	newInputs := make([]builds.Input, len(fetchedInputs))
	for i, fetched := range fetchedInputs {
		newInputs[i] = fetched.Input
		build.Config = fetched.Config.Merge(build.Config)

		defer fetched.Release()
	}

	build.Inputs = newInputs

	emitter.EmitEvent(event.Initialize{
		BuildConfig: build.Config,
	})

	container, err := builder.createBuildContainer(build.Config)
	if err != nil {
		return RunningBuild{}, builder.emitError(emitter, "failed to create container", err)
	}

	sourcesDir, err := builder.streamInResources(container, fetchedInputs, build.Config.Paths)
	if err != nil {
		return RunningBuild{}, builder.emitError(emitter, "failed to stream in resources", err)
	}

	emitter.EmitEvent(event.Start{
		Time: time.Now().Unix(),
	})

	process, err := builder.runBuild(
		container,
		sourcesDir,
		emitterProcessIO(emitter),
		build.Privileged,
		build.Config,
	)
	if err != nil {
		return RunningBuild{}, builder.emitError(emitter, "failed to run", err)
	}

	return RunningBuild{
		Build: build,

		ContainerHandle: container.Handle(),
		Container:       container,

		ProcessID: process.ID(),
		Process:   process,
	}, nil
}

func (builder *builder) Attach(running RunningBuild, emitter event.Emitter, abort <-chan struct{}) (ExitedBuild, error) {
	if running.Container == nil {
		container, err := builder.wardenClient.Lookup(running.ContainerHandle)
		if err != nil {
			return ExitedBuild{}, builder.emitError(emitter, "failed to lookup container", err)
		}

		running.Container = container
	}

	if running.Process == nil {
		process, err := running.Container.Attach(
			running.ProcessID,
			emitterProcessIO(emitter),
		)
		if err != nil {
			return ExitedBuild{}, builder.emitError(emitter, "failed to attach to process", err)
		}

		running.Process = process
	}

	status, err := builder.waitForRunToEnd(running, abort)
	if err != nil {
		return ExitedBuild{}, builder.emitError(emitter, "result unknown", err)
	}

	return ExitedBuild{
		Build:     running.Build,
		Container: running.Container,

		ExitStatus: status,
	}, nil
}

func (builder *builder) Finish(exited ExitedBuild, emitter event.Emitter, abort <-chan struct{}) (builds.Build, error) {
	emitter.EmitEvent(event.Finish{
		Time:       time.Now().Unix(),
		ExitStatus: exited.ExitStatus,
	})

	outputs, err := builder.performOutputs(exited.Container, exited, emitter, abort)
	if err != nil {
		return builds.Build{}, err
	}

	exited.Build.Outputs = outputs

	return exited.Build, nil
}

func (builder *builder) Hijack(running RunningBuild, spec warden.ProcessSpec, io warden.ProcessIO) (warden.Process, error) {
	container, err := builder.wardenClient.Lookup(running.ContainerHandle)
	if err != nil {
		return nil, err
	}

	return container.Run(spec, io)
}

func (builder *builder) emitError(emitter event.Emitter, message string, err error) error {
	emitter.EmitEvent(event.Error{
		Message: fmt.Sprintf("%s: %s", message, err),
	})

	return err
}

func (builder *builder) createBuildContainer(
	buildConfig builds.Config,
) (warden.Container, error) {
	return builder.wardenClient.Create(warden.ContainerSpec{
		RootFSPath: buildConfig.Image,
	})
}

func (builder *builder) streamInResources(
	container warden.Container,
	fetchedInputs []inputs.FetchedInput,
	paths map[string]string,
) (string, error) {
	if len(fetchedInputs) == 0 {
		return "", nil
	}

	sourcesDir := "/tmp/build/src"

	for _, input := range fetchedInputs {
		destination, found := paths[input.Input.Name]
		if !found {
			destination = input.Input.Name
		}

		err := container.StreamIn(path.Join(sourcesDir, destination), input.Stream)
		if err != nil {
			return "", err
		}
	}

	return sourcesDir, nil
}

func (builder *builder) runBuild(
	container warden.Container,
	sourcesDir string,
	processIO warden.ProcessIO,
	privileged bool,
	buildConfig builds.Config,
) (warden.Process, error) {
	env := []string{}
	for n, v := range buildConfig.Params {
		env = append(env, n+"="+v)
	}

	return container.Run(warden.ProcessSpec{
		Path: buildConfig.Run.Path,
		Args: buildConfig.Run.Args,
		Env:  env,
		Dir:  sourcesDir,

		TTY: &warden.TTYSpec{},

		Privileged: privileged,
	}, processIO)
}

func (builder *builder) waitForRunToEnd(
	running RunningBuild,
	abort <-chan struct{},
) (int, error) {
	statusCh := make(chan int, 1)
	errCh := make(chan error, 1)

	go func() {
		status, err := running.Process.Wait()
		if err != nil {
			errCh <- err
		} else {
			statusCh <- status
		}
	}()

	var runErr error

	for {
		select {
		case status := <-statusCh:
			return status, runErr

		case err := <-errCh:
			return 0, err

		case <-abort:
			abort = nil

			// delay return until process dies
			runErr = ErrAborted

			running.Container.Stop(false)
		}
	}
}

func (builder *builder) performOutputs(
	container warden.Container,
	build ExitedBuild,
	emitter event.Emitter,
	abort <-chan struct{},
) ([]builds.Output, error) {
	implicitOutputs := []builds.Output{}
	outputsToPerform := []builds.Output{}
	for _, output := range build.Build.Outputs {
		if !output.On.SatisfiedBy(build.ExitStatus) {
			continue
		}

		outputsToPerform = append(outputsToPerform, output)
	}

	performedOutputs, err := builder.outputPerformer.PerformOutputs(container, outputsToPerform, emitter, abort)
	if err != nil {
		return nil, err
	}

	return append(implicitOutputs, performedOutputs...), nil
}

func emitterProcessIO(emitter event.Emitter) warden.ProcessIO {
	return warden.ProcessIO{
		Stdout: logwriter.NewWriter(emitter, event.Origin{
			Type: event.OriginTypeRun,
			Name: "stdout",
		}),
		Stderr: logwriter.NewWriter(emitter, event.Origin{
			Type: event.OriginTypeRun,
			Name: "stderr",
		}),
	}
}
