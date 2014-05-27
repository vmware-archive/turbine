package scriptrunner

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"

	"github.com/cloudfoundry-incubator/garden/warden"
)

type ErrResourceScriptFailed struct {
	Stdout     []byte
	Stderr     []byte
	ExitStatus uint32
}

func (err ErrResourceScriptFailed) Error() string {
	return fmt.Sprintf(
		"resource script failed: exit status %d\n\nstdout:\n\n%s\n\nstderr:%s",
		err.ExitStatus,
		err.Stdout,
		err.Stderr,
	)
}

func Run(
	container warden.Container,
	script string,
	logs io.Writer,
	input interface{},
	output interface{},
) error {
	err := injectInput(container, input)
	if err != nil {
		return err
	}

	_, stream, err := container.Run(warden.ProcessSpec{
		Script: script + " < /tmp/resource-artifacts/stdin",
	})
	if err != nil {
		return err
	}

	rawOutput, err := waitForRunToEnd(stream, logs)
	if err != nil {
		return err
	}

	return json.Unmarshal(rawOutput, output)
}

func injectInput(container warden.Container, input interface{}) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}

	streamIn, err := container.StreamIn("/tmp/resource-artifacts")
	if err != nil {
		return err
	}

	tarWriter := tar.NewWriter(streamIn)

	err = tarWriter.WriteHeader(&tar.Header{
		Name: "./stdin",
		Mode: 0644,
		Size: int64(len(payload)),
	})
	if err != nil {
		return err
	}

	_, err = tarWriter.Write(payload)
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

func waitForRunToEnd(stream <-chan warden.ProcessStream, logs io.Writer) ([]byte, error) {
	stdout := []byte{}
	stderr := []byte{}

	for chunk := range stream {
		if chunk.ExitStatus != nil {
			if *chunk.ExitStatus != 0 {
				return nil, ErrResourceScriptFailed{
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
			if logs != nil {
				logs.Write(chunk.Data)
			}

			stderr = append(stderr, chunk.Data...)
		}
	}

	return stdout, nil
}
