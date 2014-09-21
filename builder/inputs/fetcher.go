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

	for i, input := range inputs {
		eventLog := logwriter.NewWriter(emitter, event.Origin{
			Type: event.OriginTypeInput,
			Name: input.Name,
		})

		resource, err := fetcher.tracker.Init(input.Type, eventLog, abort)
		if err != nil {
			return nil, err
		}

		tarStream, computedInput, buildConfig, err := resource.In(input)
		if err != nil {
			return nil, err
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
	}

	return fetchedInputs, nil
}
