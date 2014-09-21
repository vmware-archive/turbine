package inputs

import (
	"io"

	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/event"
	"github.com/concourse/turbine/logwriter"
	"github.com/concourse/turbine/resource"
)

type FetchedInput struct {
	Input   builds.Input
	Stream  io.Reader
	Config  builds.Config
	Release func() error
}

type Fetcher interface {
	Fetch([]builds.Input, event.Emitter, <-chan struct{}) ([]FetchedInput, error)
}

type parallelFetcher struct {
	tracker resource.Tracker
}

func NewParallelFetcher(tracker resource.Tracker) Fetcher {
	return &parallelFetcher{
		tracker: tracker,
	}
}

func (fetcher *parallelFetcher) Fetch(inputs []builds.Input, emitter event.Emitter, abort <-chan struct{}) ([]FetchedInput, error) {
	fetchedInputs := make([]FetchedInput, len(inputs))

	errResults := make(chan error, len(inputs))

	initializedResources := make(chan resource.Resource, len(inputs))

	for i, input := range inputs {
		go func(i int, input builds.Input) {
			eventLog := logwriter.NewWriter(emitter, event.Origin{
				Type: event.OriginTypeInput,
				Name: input.Name,
			})

			resource, err := fetcher.tracker.Init(input.Type, eventLog, abort)
			if err != nil {
				emitInputError(emitter, input, err)
				errResults <- err
				return
			}

			initializedResources <- resource

			tarStream, computedInput, buildConfig, err := resource.In(input)
			if err != nil {
				emitInputError(emitter, input, err)
				errResults <- err
				return
			}

			emitter.EmitEvent(event.Input{Input: computedInput})

			fetchedInputs[i] = FetchedInput{
				Input:  computedInput,
				Stream: tarStream,
				Config: buildConfig,
				Release: func() error {
					return fetcher.tracker.Release(resource)
				},
			}

			errResults <- nil
		}(i, input)
	}

	var fetchErr error
	for i := 0; i < len(inputs); i++ {
		err := <-errResults
		if err != nil {
			fetchErr = err
		}
	}

	if fetchErr != nil {
	dance:
		for {
			select {
			case res := <-initializedResources:
				fetcher.tracker.Release(res)
			default:
				break dance
			}
		}

		return nil, fetchErr
	}

	return fetchedInputs, nil
}

func emitInputError(emitter event.Emitter, input builds.Input, err error) {
	emitter.EmitEvent(event.Error{
		Message: err.Error(),
		Origin: event.Origin{
			Type: event.OriginTypeInput,
			Name: input.Name,
		},
	})
}
