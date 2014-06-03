package fakesourcefetcher

import (
	"bytes"
	"io"
	"sync"

	"github.com/winston-ci/prole/api/builds"
)

type FetcherFunc func(builds.Input, io.Writer, <-chan struct{}) (
	builds.Config,
	builds.Version,
	[]builds.MetadataField,
	io.Reader,
	error,
)

type Fetcher struct {
	fetched      []builds.Input
	WhenFetching FetcherFunc
	FetchError   error

	sync.RWMutex
}

func New() *Fetcher {
	return &Fetcher{}
}

func (fetcher *Fetcher) Fetch(input builds.Input, logs io.Writer, abort <-chan struct{}) (builds.Config, builds.Version, []builds.MetadataField, io.Reader, error) {
	if fetcher.FetchError != nil {
		return builds.Config{}, nil, nil, nil, fetcher.FetchError
	}

	var buildConfig builds.Config
	var fetchedVersion builds.Version
	var fetchedMetadata []builds.MetadataField
	var result io.Reader

	if fetcher.WhenFetching != nil {
		config, version, metadata, stream, err := fetcher.WhenFetching(input, logs, abort)
		if err != nil {
			return builds.Config{}, nil, nil, nil, err
		}

		buildConfig = config
		fetchedVersion = version
		fetchedMetadata = metadata
		result = stream
	} else {
		result = new(bytes.Buffer)
	}

	fetcher.Lock()
	fetcher.fetched = append(fetcher.fetched, input)
	fetcher.Unlock()

	return buildConfig, fetchedVersion, fetchedMetadata, result, nil
}

func (fetcher *Fetcher) Fetched() []builds.Input {
	fetcher.RLock()

	fetched := make([]builds.Input, len(fetcher.fetched))
	copy(fetched, fetcher.fetched)

	fetcher.RUnlock()

	return fetched
}
