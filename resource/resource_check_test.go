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

var _ = Describe("Resource Check", func() {
	var (
		input builds.Input

		checkStdout     string
		checkStderr     string
		checkExitStatus uint32
		runCheckError   error

		checkResult []builds.Version
		checkErr    error
	)

	BeforeEach(func() {
		input = builds.Input{
			Type:    "some-resource",
			Source:  builds.Source{"some": "source"},
			Version: builds.Version{"some": "version"},
		}

		checkStdout = "[]"
		checkStderr = ""
		checkExitStatus = 0
		runCheckError = nil

		checkResult = nil
		checkErr = nil
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
			return 1, checkStream, runCheckError
		}

		checkResult, checkErr = resource.Check(input)
	})

	Context("when streaming in the input configuration succeeds", func() {
		var streamedIn *gbytes.Buffer

		BeforeEach(func() {
			streamedIn = gbytes.NewBuffer()

			wardenClient.Connection.WhenStreamingIn = func(handle string, destination string, in io.Reader) error {
				Ω(handle).Should(Equal("some-handle"))
				Ω(destination).Should(Equal("/tmp/resource-artifacts"))

				_, err := io.Copy(streamedIn, in)
				Ω(err).ShouldNot(HaveOccurred())

				return nil
			}
		})

		It("creates a file with the input configuration", func() {
			Ω(checkErr).ShouldNot(HaveOccurred())

			tarReader := tar.NewReader(bytes.NewBuffer(streamedIn.Contents()))

			hdr, err := tarReader.Next()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(hdr.Name).Should(Equal("./stdin"))
			Ω(hdr.Mode).Should(Equal(int64(0644)))

			inputConfig, err := ioutil.ReadAll(tarReader)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(string(inputConfig)).Should(Equal(`{"version":{"some":"version"},"source":{"some":"source"}}`))

			_, err = tarReader.Next()
			Ω(err).Should(Equal(io.EOF))
		})

		It("runs /tmp/resource/check with the contents of the input config file on stdin", func() {
			Ω(checkErr).ShouldNot(HaveOccurred())

			Ω(wardenClient.Connection.SpawnedProcesses("some-handle")).Should(Equal([]warden.ProcessSpec{
				{
					Path: "bash",
					Args: []string{
						"-c",
						"/tmp/resource/check < /tmp/resource-artifacts/stdin",
					},
					Privileged: true,
				},
			}))
		})
	})

	Context("when /check outputs versions", func() {
		BeforeEach(func() {
			checkStdout = `[{"ver":"abc"}, {"ver":"def"}, {"ver":"ghi"}]`
		})

		It("returns the raw parsed contents", func() {
			Ω(checkErr).ShouldNot(HaveOccurred())

			Ω(checkResult).Should(Equal([]builds.Version{
				builds.Version{"ver": "abc"},
				builds.Version{"ver": "def"},
				builds.Version{"ver": "ghi"},
			}))
		})
	})

	Context("when creating the input config file fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			wardenClient.Connection.WhenStreamingIn = func(_, _ string, _ io.Reader) error {
				return disaster
			}
		})

		It("returns the error", func() {
			Ω(checkErr).Should(Equal(disaster))
		})
	})

	Context("when running /tmp/resource/check fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			runCheckError = disaster
		})

		It("returns an err containing stdout/stderr of the process", func() {
			Ω(checkErr).Should(Equal(disaster))
		})
	})

	Context("when /tmp/resource/check exits nonzero", func() {
		BeforeEach(func() {
			checkStdout = "some-stdout-data"
			checkStderr = "some-stderr-data"
			checkExitStatus = 9
		})

		It("returns an err containing stdout/stderr of the process", func() {
			Ω(checkErr).Should(HaveOccurred())

			Ω(checkErr.Error()).Should(ContainSubstring("some-stdout-data"))
			Ω(checkErr.Error()).Should(ContainSubstring("some-stderr-data"))
			Ω(checkErr.Error()).Should(ContainSubstring("exit status 9"))
		})
	})

	Context("when the output of /tmp/resource/check is malformed", func() {
		BeforeEach(func() {
			checkStdout = "ß"
		})

		It("returns an error", func() {
			Ω(checkErr).Should(HaveOccurred())
		})
	})
})
