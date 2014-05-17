package fakesourcefetcher

import (
	"bytes"
	"io"
	"sync"

	"github.com/winston-ci/prole/api/builds"
)

type Fetcher struct {
	fetched      []FetchedSpec
	WhenFetching func(builds.BuildSource, []byte) (builds.BuildConfig, io.Reader, error)
	FetchError   error

	sync.RWMutex
}

type FetchedSpec struct {
	Source  builds.BuildSource
	Payload []byte
}

func New() *Fetcher {
	return &Fetcher{}
}

func (fetcher *Fetcher) Fetch(source builds.BuildSource, payload []byte) (builds.BuildConfig, io.Reader, error) {
	if fetcher.FetchError != nil {
		return builds.BuildConfig{}, nil, fetcher.FetchError
	}

	var buildConfig builds.BuildConfig
	var result io.Reader

	if fetcher.WhenFetching != nil {
		config, stream, err := fetcher.WhenFetching(source, payload)
		if err != nil {
			return builds.BuildConfig{}, nil, err
		}

		buildConfig = config
		result = stream
	} else {
		result = new(bytes.Buffer)
	}

	fetcher.Lock()
	fetcher.fetched = append(fetcher.fetched, FetchedSpec{
		Source:  source,
		Payload: payload,
	})
	fetcher.Unlock()

	return buildConfig, result, nil
}

func (fetcher *Fetcher) Fetched() []FetchedSpec {
	fetcher.RLock()

	fetched := make([]FetchedSpec, len(fetcher.fetched))
	copy(fetched, fetcher.fetched)

	fetcher.RUnlock()

	return fetched
}
