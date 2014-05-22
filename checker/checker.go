package checker

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/config"
)

type Checker interface {
	Check(builds.Input) ([]builds.Source, error)
}

var ErrUnknownSourceType = errors.New("unknown source type")

type ErrResourceCheckFailed struct {
	Stdout     []byte
	Stderr     []byte
	ExitStatus uint32
}

func (err ErrResourceCheckFailed) Error() string {
	return fmt.Sprintf(
		"resource checking failed: exit status %d\n\nstdout:\n\n%s\n\nstderr:%s",
		err.ExitStatus,
		err.Stdout,
		err.Stderr,
	)
}

type checker struct {
	resourceTypes config.ResourceTypes
	wardenClient  warden.Client
}

func NewChecker(
	resourceTypes config.ResourceTypes,
	wardenClient warden.Client,
) Checker {
	return &checker{
		resourceTypes: resourceTypes,
		wardenClient:  wardenClient,
	}
}

func (checker *checker) Check(input builds.Input) ([]builds.Source, error) {
	resourceType, found := checker.resourceTypes.Lookup(input.Type)
	if !found {
		return nil, ErrUnknownSourceType
	}

	container, err := checker.wardenClient.Create(warden.ContainerSpec{
		RootFSPath: "docker:///" + resourceType.Image,
	})
	if err != nil {
		return nil, err
	}

	defer checker.wardenClient.Destroy(container.Handle())

	err = checker.injectInputSource(container, input.Source)
	if err != nil {
		return nil, err
	}

	_, stream, err := container.Run(warden.ProcessSpec{
		Script: "/tmp/resource/check < /tmp/resource-artifacts/input.json",
	})
	if err != nil {
		return nil, err
	}

	stdout, err := checker.waitForRunToEnd(stream)
	if err != nil {
		return nil, err
	}

	var sources []builds.Source
	err = json.Unmarshal(stdout, &sources)
	if err != nil {
		return nil, err
	}

	return sources, nil
}

func (checker *checker) injectInputSource(
	container warden.Container,
	source builds.Source,
) error {
	streamIn, err := container.StreamIn("/tmp/resource-artifacts/")
	if err != nil {
		return err
	}

	tarWriter := tar.NewWriter(streamIn)

	err = tarWriter.WriteHeader(&tar.Header{
		Name: "./input.json",
		Mode: 0644,
		Size: int64(len(source)),
	})
	if err != nil {
		return err
	}

	_, err = tarWriter.Write(source)
	if err != nil {
		return err
	}

	err = tarWriter.Close()
	if err != nil {
		return err
	}

	err = streamIn.Close()
	if err != nil {
		return err
	}

	return nil
}

func (checker *checker) waitForRunToEnd(stream <-chan warden.ProcessStream) ([]byte, error) {
	stdout := []byte{}
	stderr := []byte{}

	for chunk := range stream {
		if chunk.ExitStatus != nil {
			if *chunk.ExitStatus != 0 {
				return nil, ErrResourceCheckFailed{
					Stdout:     stdout,
					Stderr:     stderr,
					ExitStatus: *chunk.ExitStatus,
				}
			}

			break
		}

		switch chunk.Source {
		case warden.ProcessStreamSourceStdout:
			stdout = append(stdout, chunk.Data...)
		case warden.ProcessStreamSourceStderr:
			stderr = append(stderr, chunk.Data...)
		}
	}

	return stdout, nil
}
