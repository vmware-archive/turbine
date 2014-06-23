package resource_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/ioutil"

	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/concourse/turbine/api/builds"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("Resource Out", func() {
	var (
		output builds.Output

		outScriptStdout     string
		outScriptStderr     string
		outScriptExitStatus uint32
		outScriptError      error

		outOutput builds.Output
		outErr    error
	)

	BeforeEach(func() {
		output = builds.Output{
			Type:   "some-resource",
			Params: builds.Params{"some": "params"},
			Source: builds.Source{"some": "source"},

			SourcePath: "some-resource",
		}

		outScriptStdout = "{}"
		outScriptStderr = ""
		outScriptExitStatus = 0
		outScriptError = nil
	})

	JustBeforeEach(func() {
		outStream := primedStream(
			warden.ProcessStream{
				Source: warden.ProcessStreamSourceStdout,
				Data:   []byte(outScriptStdout),
			},
			warden.ProcessStream{
				Source: warden.ProcessStreamSourceStderr,
				Data:   []byte(outScriptStderr),
			},
			warden.ProcessStream{
				ExitStatus: &outScriptExitStatus,
			},
		)

		wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
			return 1, outStream, outScriptError
		}

		outOutput, outErr = resource.Out(bytes.NewBufferString("the-source"), output)
	})

	Context("when streaming the script input in succeeds", func() {
		var streamedIn *gbytes.Buffer

		BeforeEach(func() {
			streamedIn = gbytes.NewBuffer()

			wardenClient.Connection.WhenStreamingIn = func(handle string, destination string, source io.Reader) error {
				Ω(handle).Should(Equal("some-handle"))

				if destination == "/tmp/resource-artifacts" {
					_, err := io.Copy(streamedIn, source)
					Ω(err).ShouldNot(HaveOccurred())
				}

				return nil
			}
		})

		It("creates a file with the output params and source", func() {
			Ω(outErr).ShouldNot(HaveOccurred())

			tarReader := tar.NewReader(bytes.NewBuffer(streamedIn.Contents()))

			hdr, err := tarReader.Next()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(hdr.Name).Should(Equal("./stdin"))
			Ω(hdr.Mode).Should(Equal(int64(0644)))

			inputConfig, err := ioutil.ReadAll(tarReader)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(string(inputConfig)).Should(Equal(`{"params":{"some":"params"},"source":{"some":"source"}}`))

			_, err = tarReader.Next()
			Ω(err).Should(Equal(io.EOF))
		})

		It("runs /tmp/resource/out /tmp/build/src with the contents of the input config file on stdin", func() {
			Ω(outErr).ShouldNot(HaveOccurred())

			Ω(wardenClient.Connection.SpawnedProcesses("some-handle")).Should(Equal([]warden.ProcessSpec{
				{
					Script:     "/tmp/resource/out /tmp/build/src < /tmp/resource-artifacts/stdin",
					Privileged: true,
				},
			}))
		})
	})

	Context("when streaming the source in succeeds", func() {
		var streamedIn *gbytes.Buffer

		BeforeEach(func() {
			streamedIn = gbytes.NewBuffer()

			wardenClient.Connection.WhenStreamingIn = func(handle string, destination string, source io.Reader) error {
				Ω(handle).Should(Equal("some-handle"))

				if destination == "/tmp/build/src" {
					_, err := io.Copy(streamedIn, source)
					Ω(err).ShouldNot(HaveOccurred())
				}

				return nil
			}
		})

		It("writes the stream source to the destination", func() {
			Ω(outErr).ShouldNot(HaveOccurred())

			Ω(string(streamedIn.Contents())).Should(Equal("the-source"))
		})
	})

	Context("when /tmp/resource/out prints the version and metadata", func() {
		BeforeEach(func() {
			outScriptStdout = `{
					"version": {"some": "new-version"},
					"metadata": [
						{"name": "a", "value":"a-value"},
						{"name": "b","value": "b-value"}
					]
				}`
		})

		It("returns the build source printed out by /tmp/resource/out", func() {
			expectedOutput := output
			expectedOutput.Version = builds.Version{"some": "new-version"}
			expectedOutput.Metadata = []builds.MetadataField{
				{Name: "a", Value: "a-value"},
				{Name: "b", Value: "b-value"},
			}

			Ω(outOutput).Should(Equal(expectedOutput))
		})
	})

	Context("when /out outputs to stderr", func() {
		BeforeEach(func() {
			outScriptStderr = "some stderr data"
		})

		It("emits it to the log sink", func() {
			Ω(outErr).ShouldNot(HaveOccurred())

			Ω(string(logs.Contents())).Should(Equal("some stderr data"))
		})
	})

	Context("when streaming in the params config fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			wardenClient.Connection.WhenStreamingIn = func(_, destination string, source io.Reader) error {
				if destination == "/tmp/resource-artifacts" {
					return disaster
				} else {
					return nil
				}
			}
		})

		It("returns the error", func() {
			Ω(outErr).Should(Equal(disaster))
		})
	})

	Context("when streaming in the source fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			wardenClient.Connection.WhenStreamingIn = func(_, destination string, source io.Reader) error {
				if destination == "/tmp/build/src" {
					return disaster
				} else {
					return nil
				}
			}
		})

		It("returns the error", func() {
			Ω(outErr).Should(Equal(disaster))
		})
	})

	Context("when running /tmp/resource/out fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			outScriptError = disaster
		})

		It("returns an err containing stdout/stderr of the process", func() {
			Ω(outErr).Should(Equal(disaster))
		})
	})

	Context("when /tmp/resource/out exits nonzero", func() {
		BeforeEach(func() {
			outScriptStdout = "some-stdout-data"
			outScriptStderr = "some-stderr-data"
			outScriptExitStatus = 9
		})

		It("returns an err containing stdout/stderr of the process", func() {
			Ω(outErr).Should(HaveOccurred())
			Ω(outErr.Error()).Should(ContainSubstring("some-stdout-data"))
			Ω(outErr.Error()).Should(ContainSubstring("some-stderr-data"))
			Ω(outErr.Error()).Should(ContainSubstring("exit status 9"))
		})
	})

	Context("when aborting", func() {
		BeforeEach(func() {
			wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
				// cause reading from the stream to block so that it can be
				// aborted
				return 1, nil, nil
			}
		})

		It("stops the container", func() {
			go resource.Out(bytes.NewBufferString("the-source"), output)

			close(abort)

			Eventually(func() interface{} {
				return wardenClient.Connection.Stopped("some-handle")
			}).Should(HaveLen(1))
		})
	})
})
