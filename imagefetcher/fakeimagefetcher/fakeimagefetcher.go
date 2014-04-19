package fakeimagefetcher

import "sync"

type Fetcher struct {
	fetched     []string
	FetchResult string
	FetchError  error

	sync.RWMutex
}

func New() *Fetcher {
	return &Fetcher{}
}

func (fetcher *Fetcher) Fetch(name string) (string, error) {
	if fetcher.FetchError != nil {
		return "", fetcher.FetchError
	}

	fetcher.Lock()
	fetcher.fetched = append(fetcher.fetched, name)
	fetcher.Unlock()

	return fetcher.FetchResult, nil
}

func (fetcher *Fetcher) Fetched() []string {
	fetcher.RLock()

	fetched := make([]string, len(fetcher.fetched))
	copy(fetched, fetcher.fetched)

	fetcher.RUnlock()

	return fetched
}
