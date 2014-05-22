package checker_test

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
	. "github.com/winston-ci/prole/checker"
	"github.com/winston-ci/prole/config"
)

var _ = Describe("Checker", func() {
	var (
		resourceTypes config.ResourceTypes
		wardenClient  *fake_warden_client.FakeClient
		checker       Checker

		input builds.Input

		checkStdout     string
		checkStderr     string
		checkExitStatus uint32
		checkError      error
	)

	primedStream := func(payloads ...warden.ProcessStream) <-chan warden.ProcessStream {
		stream := make(chan warden.ProcessStream, len(payloads))

		for _, payload := range payloads {
			stream <- payload
		}

		close(stream)

		return stream
	}

	BeforeEach(func() {
		resourceTypes = config.ResourceTypes{}
		wardenClient = fake_warden_client.New()

		input = builds.Input{
			Type:   "some-resource",
			Source: builds.Source("some-source"),
		}

		checkStdout = "[]"
		checkStderr = ""
		checkExitStatus = 0
		checkError = nil

		wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
			return "some-handle", nil
		}
	})

	JustBeforeEach(func() {
		checkStream := primedStream(
			warden.ProcessStream{
				Source: warden.ProcessStreamSourceStdout,
				Data:   []byte(checkStdout),
			},
			warden.ProcessStream{
				Source: warden.ProcessStreamSourceStderr,
				Data:   []byte(checkStderr),
			},
			warden.ProcessStream{
				ExitStatus: &checkExitStatus,
			},
		)

		wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
			return 1, checkStream, checkError
		}

		checker = NewChecker(resourceTypes, wardenClient)
	})

	Context("when the source's resource type is configured", func() {
		BeforeEach(func() {
			resourceTypes = append(resourceTypes, config.ResourceType{
				Name:  "some-resource",
				Image: "some-resource-image",
			})
		})

		It("creates a container with the image configured via the source's type", func() {
			_, err := checker.Check(input)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(wardenClient.Connection.Created()).Should(Equal([]warden.ContainerSpec{
				{
					RootFSPath: "docker:///some-resource-image",
				},
			}))
		})

		It("creates a file with the input configuration", func() {
			buffer := gbytes.NewBuffer()

			wardenClient.Connection.WhenStreamingIn = func(handle string, destination string) (io.WriteCloser, error) {
				Ω(handle).Should(Equal("some-handle"))
				Ω(destination).Should(Equal("/tmp/resource-artifacts/"))
				return buffer, nil
			}

			_, err := checker.Check(input)
			Ω(err).ShouldNot(HaveOccurred())

			tarReader := tar.NewReader(bytes.NewBuffer(buffer.Contents()))

			hdr, err := tarReader.Next()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(hdr.Name).Should(Equal("./input.json"))
			Ω(hdr.Mode).Should(Equal(int64(0644)))

			inputConfig, err := ioutil.ReadAll(tarReader)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(string(inputConfig)).Should(Equal("some-source"))

			_, err = tarReader.Next()
			Ω(err).Should(Equal(io.EOF))

			Ω(buffer.Closed()).Should(BeTrue())
		})

		It("runs /tmp/resource/check with the contents of the input config file on stdin", func() {
			_, err := checker.Check(input)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(wardenClient.Connection.SpawnedProcesses("some-handle")).Should(Equal([]warden.ProcessSpec{
				{
					Script: "/tmp/resource/check < /tmp/resource-artifacts/input.json",
				},
			}))
		})

		It("destroys the container", func() {
			_, err := checker.Check(input)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(wardenClient.Connection.Destroyed()).Should(ContainElement("some-handle"))
		})

		Context("when /check outputs versions", func() {
			BeforeEach(func() {
				checkStdout = `["abc", "def", "ghi"]`
			})

			It("returns the raw parsed contents", func() {
				sources, err := checker.Check(input)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(sources).Should(Equal([]builds.Source{
					builds.Source(`"abc"`),
					builds.Source(`"def"`),
					builds.Source(`"ghi"`),
				}))
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
				_, err := checker.Check(input)
				Ω(err).Should(Equal(disaster))
			})
		})

		Context("when creating the input config file fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				wardenClient.Connection.WhenStreamingIn = func(_, _ string) (io.WriteCloser, error) {
					return nil, disaster
				}
			})

			It("returns the error", func() {
				_, err := checker.Check(input)
				Ω(err).Should(Equal(disaster))
			})
		})

		Context("when running /tmp/resource/check fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				checkError = disaster
			})

			It("returns an err containing stdout/stderr of the process", func() {
				_, err := checker.Check(input)
				Ω(err).Should(Equal(disaster))
			})
		})

		Context("when /tmp/resource/check exits nonzero", func() {
			BeforeEach(func() {
				checkStdout = "some-stdout-data"
				checkStderr = "some-stderr-data"
				checkExitStatus = 9
			})

			It("returns an err containing stdout/stderr of the process", func() {
				_, err := checker.Check(input)
				Ω(err).Should(HaveOccurred())
				Ω(err.Error()).Should(ContainSubstring("some-stdout-data"))
				Ω(err.Error()).Should(ContainSubstring("some-stderr-data"))
				Ω(err.Error()).Should(ContainSubstring("exit status 9"))
			})
		})

		Context("when the output of /tmp/resource/check is malformed", func() {
			BeforeEach(func() {
				checkStdout = "ß"
			})

			It("returns an error", func() {
				_, err := checker.Check(input)
				Ω(err).Should(HaveOccurred())
			})
		})
	})

	Context("when the source's resource type is unknown", func() {
		It("returns ErrUnknownSourceType", func() {
			_, err := checker.Check(builds.Input{
				Type: "lol-butts",
			})
			Ω(err).Should(Equal(ErrUnknownSourceType))
		})

		It("does not create a container", func() {
			_, err := checker.Check(builds.Input{
				Type: "lol-butts",
			})
			Ω(err).Should(HaveOccurred())

			Ω(wardenClient.Connection.Created()).Should(BeEmpty())
		})
	})
})
