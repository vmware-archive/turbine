package sourcefetcher

import (
	"io"
	"io/ioutil"
	"net/http"

	"github.com/pivotal-golang/archiver/extractor"

	"github.com/winston-ci/prole/api/builds"
)

type SourceFetcher struct {
	tmpdir string

	extractor extractor.Extractor

	httpClient *http.Client
}

func NewSourceFetcher(tmpdir string, extractor extractor.Extractor) *SourceFetcher {
	return &SourceFetcher{
		tmpdir:    tmpdir,
		extractor: extractor,

		httpClient: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
	}
}

func (fetcher *SourceFetcher) Fetch(source builds.BuildSource) (string, error) {
	response, err := fetcher.httpClient.Get(source.URI)
	if err != nil {
		return "", err
	}

	defer response.Body.Close()

	tempFile, err := ioutil.TempFile(fetcher.tmpdir, "fetched-file")
	if err != nil {
		return "", err
	}

	_, err = io.Copy(tempFile, response.Body)
	if err != nil {
		tempFile.Close()
		return "", err
	}

	tempFile.Close()

	tempDir, err := ioutil.TempDir(fetcher.tmpdir, "fetched-contents")
	if err != nil {
		return "", err
	}

	err = fetcher.extractor.Extract(tempFile.Name(), tempDir)
	if err != nil {
		return "", err
	}

	return tempDir, nil
}
