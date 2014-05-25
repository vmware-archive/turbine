package outputter

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"

	"github.com/cloudfoundry-incubator/garden/warden"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/config"
)

var ErrUnknownSourceType = errors.New("unknown source type")

type ErrOutputFailed struct {
	Stdout     []byte
	Stderr     []byte
	ExitStatus uint32
}

func (err ErrOutputFailed) Error() string {
	return fmt.Sprintf(
		"output failed: exit status %d\n\nstdout:\n\n%s\n\nstderr:%s",
		err.ExitStatus,
		err.Stdout,
		err.Stderr,
	)
}

type Outputter struct {
	resourceTypes config.ResourceTypes
	wardenClient  warden.Client
}

func NewOutputter(
	resourceTypes config.ResourceTypes,
	wardenClient warden.Client,
) *Outputter {
	return &Outputter{
		resourceTypes: resourceTypes,
		wardenClient:  wardenClient,
	}
}

func (outputter *Outputter) PerformOutput(output builds.Output, sourceStream io.Reader) (builds.Source, error) {
	resourceType, found := outputter.resourceTypes.Lookup(output.Type)
	if !found {
		return nil, ErrUnknownSourceType
	}

	container, err := outputter.wardenClient.Create(warden.ContainerSpec{
		RootFSPath: "docker:///" + resourceType.Image,
	})
	if err != nil {
		return nil, err
	}

	err = outputter.streamInSource(container, sourceStream)
	if err != nil {
		return nil, err
	}

	err = outputter.injectOutputParams(container, output.Params)
	if err != nil {
		return nil, err
	}

	_, stream, err := container.Run(warden.ProcessSpec{
		Script: "/tmp/resource/out /tmp/build/src < /tmp/resource-artifacts/output.json",
	})
	if err != nil {
		return nil, err
	}

	source, err := outputter.waitForRunToEnd(stream)
	if err != nil {
		return nil, err
	}

	return source, err
}

func (outputter *Outputter) streamInSource(
	container warden.Container,
	sourceStream io.Reader,
) error {
	streamIn, err := container.StreamIn("/tmp/build/src")
	if err != nil {
		return err
	}

	_, err = io.Copy(streamIn, sourceStream)
	if err != nil {
		return err
	}

	return streamIn.Close()
}

func (outputter *Outputter) injectOutputParams(
	container warden.Container,
	params builds.Params,
) error {
	streamIn, err := container.StreamIn("/tmp/resource-artifacts")
	if err != nil {
		return err
	}

	tarWriter := tar.NewWriter(streamIn)

	err = tarWriter.WriteHeader(&tar.Header{
		Name: "./output.json",
		Mode: 0644,
		Size: int64(len(params)),
	})
	if err != nil {
		return err
	}

	_, err = tarWriter.Write(params)
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

func (outputter *Outputter) waitForRunToEnd(stream <-chan warden.ProcessStream) (builds.Source, error) {
	stdout := []byte{}
	stderr := []byte{}

	for chunk := range stream {
		if chunk.ExitStatus != nil {
			if *chunk.ExitStatus != 0 {
				return nil, ErrOutputFailed{
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

	return builds.Source(stdout), nil
}
