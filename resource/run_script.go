package resource

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

func (resource *resource) runScript(script string, input interface{}, output interface{}) error {
	err := resource.injectInput(input)
	if err != nil {
		return err
	}

	_, stream, err := resource.container.Run(warden.ProcessSpec{
		Path: "bash",
		Args: []string{
			"-c",
			script + " < /tmp/resource-artifacts/stdin",
		},
		Privileged: true,
	})
	if err != nil {
		return err
	}

	rawOutput, err := resource.waitForRunToEnd(stream)
	if err != nil {
		return err
	}

	return json.Unmarshal(rawOutput, output)
}

func (resource *resource) injectInput(input interface{}) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}

	tarRead, tarWrite := io.Pipe()

	go func() {
		defer tarWrite.Close()

		tarWriter := tar.NewWriter(tarWrite)

		tarWriter.WriteHeader(&tar.Header{
			Name: "./stdin",
			Mode: 0644,
			Size: int64(len(payload)),
		})
		if err != nil {
			return
		}

		_, err = tarWriter.Write(payload)
		if err != nil {
			return
		}

		err = tarWriter.Close()
		if err != nil {
			return
		}
	}()

	return resource.container.StreamIn("/tmp/resource-artifacts", tarRead)
}

func (resource *resource) waitForRunToEnd(stream <-chan warden.ProcessStream) ([]byte, error) {
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
				if resource.logs != nil {
					resource.logs.Write(chunk.Data)
				}

				stderr = append(stderr, chunk.Data...)
			}
		case <-resource.abort:
			resource.container.Stop(false)
			return nil, ErrAborted
		}
	}

	return stdout, nil
}
