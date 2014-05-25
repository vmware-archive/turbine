package fakeoutputter

import (
	"io"
	"sync"

	"github.com/onsi/gomega/gbytes"
	"github.com/winston-ci/prole/api/builds"
)

type Outputter struct {
	performedOutputs     []PerformSpec
	WhenPerformingOutput func(builds.Output, io.Reader) (builds.Source, error)
	PerformOutputError   error

	sync.RWMutex
}

type PerformSpec struct {
	Output     builds.Output
	StreamedIn *gbytes.Buffer
}

func New() *Outputter {
	return &Outputter{
		WhenPerformingOutput: func(builds.Output, io.Reader) (builds.Source, error) {
			return nil, nil
		},
	}
}

func (outputter *Outputter) PerformOutput(output builds.Output, streamIn io.Reader) (builds.Source, error) {
	if outputter.PerformOutputError != nil {
		return nil, outputter.PerformOutputError
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
	})
	outputter.Unlock()

	return outputter.WhenPerformingOutput(output, streamIn)
}

func (outputter *Outputter) PerformedOutputs() []PerformSpec {
	outputter.RLock()
	defer outputter.RLock()

	return outputter.performedOutputs
}
