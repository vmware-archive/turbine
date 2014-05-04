package sourcefetcher

import (
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"os/exec"

	"github.com/cloudfoundry/gunk/command_runner"
	"github.com/pivotal-golang/archiver/extractor"

	"github.com/winston-ci/prole/api/builds"
)

var ErrUnknownSourceType = errors.New("unknown source type")

type SourceFetcher struct {
	tmpdir string

	extractor     extractor.Extractor
	commandRunner command_runner.CommandRunner

	httpClient *http.Client
}

func NewSourceFetcher(
	tmpdir string,
	extractor extractor.Extractor,
	commandRunner command_runner.CommandRunner,
) *SourceFetcher {
	return &SourceFetcher{
		tmpdir:        tmpdir,
		extractor:     extractor,
		commandRunner: commandRunner,

		httpClient: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
	}
}

func (fetcher *SourceFetcher) Fetch(source builds.BuildSource) (string, error) {
	switch source.Type {
	case "raw":
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

	case "git":
		tempDir, err := ioutil.TempDir(fetcher.tmpdir, "cloned-contents")
		if err != nil {
			return "", err
		}

		clone := &exec.Cmd{
			Path: "git",
			Args: []string{
				"clone",
				"--depth", "10",
				"--branch", source.Branch,
				source.URI,
				tempDir,
			},
		}

		err = fetcher.commandRunner.Run(clone)
		if err != nil {
			return "", err
		}

		checkout := &exec.Cmd{
			Path: "git",
			Args: []string{"checkout", source.Ref},
			Dir:  tempDir,
		}

		err = fetcher.commandRunner.Run(checkout)
		if err != nil {
			return "", err
		}

		return tempDir, nil
	}

	return "", ErrUnknownSourceType
}
