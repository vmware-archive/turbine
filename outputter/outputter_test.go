package outputter_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/ioutil"

	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/config"
	. "github.com/winston-ci/prole/outputter"
)

var _ = Describe("Outputter", func() {
	var (
		resourceTypes config.ResourceTypes
		wardenClient  *fake_warden_client.FakeClient
		outputter     *Outputter

		output builds.Output
		logs   io.Writer

		outStdout     string
		outStderr     string
		outExitStatus uint32
		outError      error

		outputVersion  builds.Version
		outputMetadata []builds.MetadataField
		outputError    error
	)

	BeforeEach(func() {
		resourceTypes = config.ResourceTypes{}
		wardenClient = fake_warden_client.New()

		output = builds.Output{
			Type:   "some-resource",
			Params: builds.Params{"some": "params"},

			SourcePath: "some-resource",
		}

		logs = nil

		wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
			return "some-handle", nil
		}

		outStdout = "{}"
		outStderr = ""
		outExitStatus = 0
		outError = nil
	})

	primedStream := func(payloads ...warden.ProcessStream) <-chan warden.ProcessStream {
		stream := make(chan warden.ProcessStream, len(payloads))

		for _, payload := range payloads {
			stream <- payload
		}

		close(stream)

		return stream
	}

	JustBeforeEach(func() {
		outStream := primedStream(
			warden.ProcessStream{
				Source: warden.ProcessStreamSourceStdout,
				Data:   []byte(outStdout),
			},
			warden.ProcessStream{
				Source: warden.ProcessStreamSourceStderr,
				Data:   []byte(outStderr),
			},
			warden.ProcessStream{
				ExitStatus: &outExitStatus,
			},
		)

		wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
			return 1, outStream, outError
		}

		outputter = NewOutputter(resourceTypes, wardenClient)

		outputVersion, outputMetadata, outputError = outputter.PerformOutput(output, bytes.NewBufferString("the-source"), logs)
	})

	Context("when the source's resource type is configured", func() {
		BeforeEach(func() {
			resourceTypes = append(resourceTypes, config.ResourceType{
				Name:  "some-resource",
				Image: "some-resource-image",
			})
		})

		It("creates a container with the image configured via the source's type", func() {
			Ω(wardenClient.Connection.Created()).Should(Equal([]warden.ContainerSpec{
				{
					RootFSPath: "docker:///some-resource-image",
				},
			}))
		})

		Context("when streaming the params in succeeds", func() {
			var streamedIn *gbytes.Buffer

			BeforeEach(func() {
				streamedIn = gbytes.NewBuffer()

				wardenClient.Connection.WhenStreamingIn = func(handle string, destination string) (io.WriteCloser, error) {
					Ω(handle).Should(Equal("some-handle"))

					if destination == "/tmp/resource-artifacts" {
						return streamedIn, nil
					} else {
						return gbytes.NewBuffer(), nil
					}
				}
			})

			It("creates a file with the output params", func() {
				Ω(outputError).ShouldNot(HaveOccurred())

				tarReader := tar.NewReader(bytes.NewBuffer(streamedIn.Contents()))

				hdr, err := tarReader.Next()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(hdr.Name).Should(Equal("./stdin"))
				Ω(hdr.Mode).Should(Equal(int64(0644)))

				inputConfig, err := ioutil.ReadAll(tarReader)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(string(inputConfig)).Should(Equal(`{"params":{"some":"params"}}`))

				_, err = tarReader.Next()
				Ω(err).Should(Equal(io.EOF))

				Ω(streamedIn.Closed()).Should(BeTrue())
			})
		})

		Context("when streaming the source in succeeds", func() {
			var streamedIn *gbytes.Buffer

			BeforeEach(func() {
				streamedIn = gbytes.NewBuffer()

				wardenClient.Connection.WhenStreamingIn = func(handle string, destination string) (io.WriteCloser, error) {
					Ω(handle).Should(Equal("some-handle"))

					if destination == "/tmp/build/src" {
						return streamedIn, nil
					} else {
						return gbytes.NewBuffer(), nil
					}
				}
			})

			It("writes the stream source to the destination", func() {
				Ω(outputError).ShouldNot(HaveOccurred())

				Ω(string(streamedIn.Contents())).Should(Equal("the-source"))
			})
		})

		It("runs /tmp/resource/out <path> with the contents of the input config file on stdin", func() {
			Ω(outputError).ShouldNot(HaveOccurred())

			Ω(wardenClient.Connection.SpawnedProcesses("some-handle")).Should(Equal([]warden.ProcessSpec{
				{
					Script: "/tmp/resource/out /tmp/build/src/some-resource < /tmp/resource-artifacts/stdin",
				},
			}))
		})

		Context("when /tmp/resource/out prints the version and metadata", func() {
			BeforeEach(func() {
				outStdout = `{
					"version": {"some": "new-version"},
					"metadata": [
						{"name": "a", "value":"a-value"},
						{"name": "b","value": "b-value"}
					]
				}`
			})

			It("returns the build source printed out by /tmp/resource/out", func() {
				Ω(outputVersion).Should(Equal(builds.Version{"some": "new-version"}))
				Ω(outputMetadata).Should(Equal([]builds.MetadataField{
					{Name: "a", Value: "a-value"},
					{Name: "b", Value: "b-value"},
				}))
			})
		})

		Context("when /out outputs to stderr", func() {
			var logBuffer *gbytes.Buffer

			BeforeEach(func() {
				outStderr = "some stderr data"

				logBuffer = gbytes.NewBuffer()
				logs = logBuffer
			})

			It("emits it to the log sink", func() {
				Ω(outputError).ShouldNot(HaveOccurred())

				Ω(string(logBuffer.Contents())).Should(Equal("some stderr data"))
			})
		})

		Context("when creating the container fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
					return "", disaster
				}
			})

			It("returns the error", func() {
				Ω(outputError).Should(Equal(disaster))
			})
		})

		Context("when streaming in the params config fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				wardenClient.Connection.WhenStreamingIn = func(_, destination string) (io.WriteCloser, error) {
					if destination == "/tmp/resource-artifacts" {
						return nil, disaster
					} else {
						return gbytes.NewBuffer(), nil
					}
				}
			})

			It("returns the error", func() {
				Ω(outputError).Should(Equal(disaster))
			})
		})

		Context("when streaming in the source fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				wardenClient.Connection.WhenStreamingIn = func(_, destination string) (io.WriteCloser, error) {
					if destination == "/tmp/build/src" {
						return nil, disaster
					} else {
						return gbytes.NewBuffer(), nil
					}
				}
			})

			It("returns the error", func() {
				Ω(outputError).Should(Equal(disaster))
			})
		})

		Context("when running /tmp/resource/out fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				outError = disaster
			})

			It("returns an err containing stdout/stderr of the process", func() {
				Ω(outputError).Should(Equal(disaster))
			})
		})

		Context("when /tmp/resource/out exits nonzero", func() {
			BeforeEach(func() {
				outStdout = "some-stdout-data"
				outStderr = "some-stderr-data"
				outExitStatus = 9
			})

			It("returns an err containing stdout/stderr of the process", func() {
				Ω(outputError).Should(HaveOccurred())
				Ω(outputError.Error()).Should(ContainSubstring("some-stdout-data"))
				Ω(outputError.Error()).Should(ContainSubstring("some-stderr-data"))
				Ω(outputError.Error()).Should(ContainSubstring("exit status 9"))
			})
		})
	})

	Context("when the source's resource type is unknown", func() {
		BeforeEach(func() {
			output.Type = "lol-butts"
		})

		It("returns ErrUnknownSourceType", func() {
			Ω(outputError).Should(Equal(ErrUnknownSourceType))
		})

		It("does not create a container", func() {
			Ω(wardenClient.Connection.Created()).Should(BeEmpty())
		})
	})
})
