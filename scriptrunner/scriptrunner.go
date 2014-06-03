package scriptrunner

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cloudfoundry-incubator/garden/warden"
)

var ErrAborted = errors.New("script aborted")

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
	abort <-chan struct{},
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

	rawOutput, err := waitForRunToEnd(container, stream, logs, abort)
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

func waitForRunToEnd(container warden.Container, stream <-chan warden.ProcessStream, logs io.Writer, abort <-chan struct{}) ([]byte, error) {
	stdout := []byte{}
	stderr := []byte{}

script:
	for {
		select {
		case chunk := <-stream:
			if chunk.ExitStatus != nil {
				if *chunk.ExitStatus != 0 {
					return nil, ErrResourceScriptFailed{
						Stdout:     stdout,
						Stderr:     stderr,
						ExitStatus: *chunk.ExitStatus,
					}
				}

				break script
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
		case <-abort:
			container.Stop(false)
			return nil, ErrAborted
		}
	}

	return stdout, nil
}
