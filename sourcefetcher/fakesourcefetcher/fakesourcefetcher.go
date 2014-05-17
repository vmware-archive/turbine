package fakesourcefetcher

import (
	"bytes"
	"io"
	"sync"

	"github.com/winston-ci/prole/api/builds"
)

type Fetcher struct {
	fetched      []builds.Input
	WhenFetching func(builds.Input) (builds.BuildConfig, io.Reader, error)
	FetchError   error

	sync.RWMutex
}

func New() *Fetcher {
	return &Fetcher{}
}

func (fetcher *Fetcher) Fetch(input builds.Input) (builds.BuildConfig, io.Reader, error) {
	if fetcher.FetchError != nil {
		return builds.BuildConfig{}, nil, fetcher.FetchError
	}

	var buildConfig builds.BuildConfig
	var result io.Reader

	if fetcher.WhenFetching != nil {
		config, stream, err := fetcher.WhenFetching(input)
		if err != nil {
			return builds.BuildConfig{}, nil, err
		}

		buildConfig = config
		result = stream
	} else {
		result = new(bytes.Buffer)
	}

	fetcher.Lock()
	fetcher.fetched = append(fetcher.fetched, input)
	fetcher.Unlock()

	return buildConfig, result, nil
}

func (fetcher *Fetcher) Fetched() []builds.Input {
	fetcher.RLock()

	fetched := make([]builds.Input, len(fetcher.fetched))
	copy(fetched, fetcher.fetched)

	fetcher.RUnlock()

	return fetched
}
