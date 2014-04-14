package fakesourcefetcher

import (
	"sync"

	"github.com/room101-ci/agent/api/builds"
)

type Fetcher struct {
	fetched     []builds.BuildSource
	FetchResult string
	FetchError  error

	sync.RWMutex
}

func New() *Fetcher {
	return &Fetcher{}
}

func (fetcher *Fetcher) Fetch(source builds.BuildSource) (string, error) {
	if fetcher.FetchError != nil {
		return "", fetcher.FetchError
	}

	fetcher.Lock()
	fetcher.fetched = append(fetcher.fetched, source)
	fetcher.Unlock()

	return fetcher.FetchResult, nil
}

func (fetcher *Fetcher) Fetched() []builds.BuildSource {
	fetcher.RLock()

	fetched := make([]builds.BuildSource, len(fetcher.fetched))
	copy(fetched, fetcher.fetched)

	fetcher.RUnlock()

	return fetched
}
