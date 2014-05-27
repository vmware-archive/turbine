package fakesourcefetcher

import (
	"bytes"
	"io"
	"sync"

	"github.com/winston-ci/prole/api/builds"
)

type Fetcher struct {
	fetched      []builds.Input
	WhenFetching func(builds.Input, io.Writer) (builds.Config, builds.Source, io.Reader, error)
	FetchError   error

	sync.RWMutex
}

func New() *Fetcher {
	return &Fetcher{}
}

func (fetcher *Fetcher) Fetch(input builds.Input, logs io.Writer) (builds.Config, builds.Source, io.Reader, error) {
	if fetcher.FetchError != nil {
		return builds.Config{}, nil, nil, fetcher.FetchError
	}

	var buildConfig builds.Config
	var fetchedSource builds.Source
	var result io.Reader

	if fetcher.WhenFetching != nil {
		config, source, stream, err := fetcher.WhenFetching(input, logs)
		if err != nil {
			return builds.Config{}, nil, nil, err
		}

		buildConfig = config
		fetchedSource = source
		result = stream
	} else {
		result = new(bytes.Buffer)
	}

	fetcher.Lock()
	fetcher.fetched = append(fetcher.fetched, input)
	fetcher.Unlock()

	return buildConfig, fetchedSource, result, nil
}

func (fetcher *Fetcher) Fetched() []builds.Input {
	fetcher.RLock()

	fetched := make([]builds.Input, len(fetcher.fetched))
	copy(fetched, fetcher.fetched)

	fetcher.RUnlock()

	return fetched
}
