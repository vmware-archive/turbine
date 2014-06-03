package fakeoutputter

import (
	"io"
	"sync"

	"github.com/onsi/gomega/gbytes"
	"github.com/winston-ci/prole/api/builds"
)

type OutputterFunc func(builds.Output, io.Reader, io.Writer, <-chan struct{}) (builds.Version, []builds.MetadataField, error)

type Outputter struct {
	performedOutputs     []PerformSpec
	WhenPerformingOutput OutputterFunc
	PerformOutputError   error

	sync.RWMutex
}

type PerformSpec struct {
	Output     builds.Output
	StreamedIn *gbytes.Buffer
	Logs       io.Writer
}

func New() *Outputter {
	return &Outputter{
		WhenPerformingOutput: func(builds.Output, io.Reader, io.Writer, <-chan struct{}) (builds.Version, []builds.MetadataField, error) {
			return nil, nil, nil
		},
	}
}

func (outputter *Outputter) PerformOutput(output builds.Output, streamIn io.Reader, logs io.Writer, abort <-chan struct{}) (builds.Version, []builds.MetadataField, error) {
	if outputter.PerformOutputError != nil {
		return nil, nil, outputter.PerformOutputError
	}

	streamedIn := gbytes.NewBuffer()

	go func() {
		io.Copy(streamedIn, streamIn)
		streamedIn.Close()
	}()

	outputter.Lock()
	outputter.performedOutputs = append(outputter.performedOutputs, PerformSpec{
		Output:     output,
		StreamedIn: streamedIn,
		Logs:       logs,
	})
	outputter.Unlock()

	return outputter.WhenPerformingOutput(output, streamIn, logs, abort)
}

func (outputter *Outputter) PerformedOutputs() []PerformSpec {
	outputter.RLock()
	defer outputter.RLock()

	return outputter.performedOutputs
}
