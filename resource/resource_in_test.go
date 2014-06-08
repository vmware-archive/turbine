package resource_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/ioutil"

	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/winston-ci/prole/api/builds"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("Resource", func() {
	Describe("In", func() {
		var (
			input builds.Input

			inStdout     string
			inStderr     string
			inExitStatus uint32
			inError      error

			fetchedStream io.Reader
			fetchedConfig builds.Config
			fetchedInput  builds.Input
			fetchError    error
		)

		BeforeEach(func() {
			input = builds.Input{
				Type:    "some-resource",
				Source:  builds.Source{"some": "source"},
				Version: builds.Version{"some": "version"},
			}

			inStdout = "{}"
			inStderr = ""
			inExitStatus = 0
			inError = nil
		})

		JustBeforeEach(func() {
			inStream := primedStream(
				warden.ProcessStream{
					Source: warden.ProcessStreamSourceStdout,
					Data:   []byte(inStdout),
				},
				warden.ProcessStream{
					Source: warden.ProcessStreamSourceStderr,
					Data:   []byte(inStderr),
				},
				warden.ProcessStream{
					ExitStatus: &inExitStatus,
				},
			)

			wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
				return 1, inStream, inError
			}

			fetchedStream, fetchedInput, fetchedConfig, fetchError = resource.In(input)
		})

		Context("when streaming succeeds", func() {
			var streamedIn *gbytes.Buffer

			BeforeEach(func() {
				streamedIn = gbytes.NewBuffer()

				wardenClient.Connection.WhenStreamingIn = func(handle string, destination string) (io.WriteCloser, error) {
					Ω(handle).Should(Equal("some-handle"))
					Ω(destination).Should(Equal("/tmp/resource-artifacts"))
					return streamedIn, nil
				}
			})

			It("creates a file with the input configuration", func() {
				Ω(fetchError).ShouldNot(HaveOccurred())

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

				Ω(streamedIn.Closed()).Should(BeTrue())
			})
		})

		It("runs /tmp/resource/in <path> with the contents of the input config file on stdin", func() {
			Ω(fetchError).ShouldNot(HaveOccurred())

			Ω(wardenClient.Connection.SpawnedProcesses("some-handle")).Should(Equal([]warden.ProcessSpec{
				{
					Script: "/tmp/resource/in /tmp/resource-destination < /tmp/resource-artifacts/stdin",
				},
			}))
		})

		Context("when /tmp/resource/in prints the source", func() {
			BeforeEach(func() {
				inStdout = `{
					"version": {"some": "new-version"},
					"metadata": [
						{"name": "a", "value":"a-value"},
						{"name": "b","value": "b-value"}
					]
				}`
			})

			It("returns the build source printed out by /tmp/resource/in", func() {
				expectedFetchedInput := input
				expectedFetchedInput.Version = builds.Version{"some": "new-version"}
				expectedFetchedInput.Metadata = []builds.MetadataField{
					{Name: "a", Value: "a-value"},
					{Name: "b", Value: "b-value"},
				}

				Ω(fetchedInput).Should(Equal(expectedFetchedInput))
			})
		})

		Context("when /in outputs to stderr", func() {
			BeforeEach(func() {
				inStderr = "some stderr data"
			})

			It("emits it to the log sink", func() {
				Ω(fetchError).ShouldNot(HaveOccurred())

				Ω(string(logs.Contents())).Should(Equal("some stderr data"))
			})
		})

		Context("when streaming out succeeds", func() {
			BeforeEach(func() {
				wardenClient.Connection.WhenStreamingOut = func(handle string, source string) (io.Reader, error) {
					Ω(handle).Should(Equal("some-handle"))

					streamOut := new(bytes.Buffer)

					if source == "/tmp/resource-destination/" {
						streamOut.WriteString("sup")
					}

					return streamOut, nil
				}
			})

			It("returns the output stream of /tmp/resource-destination/", func() {
				contents, err := ioutil.ReadAll(fetchedStream)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(string(contents)).Should(Equal("sup"))
			})
		})

		Context("when a config path is specified", func() {
			BeforeEach(func() {
				input.ConfigPath = "some/config/path.yml"
			})

			Context("and the config path exists", func() {
				BeforeEach(func() {
					wardenClient.Connection.WhenStreamingOut = func(handle string, src string) (io.Reader, error) {
						Ω(handle).Should(Equal("some-handle"))

						buf := new(bytes.Buffer)

						if src == "/tmp/resource-destination/some/config/path.yml" {
							tarWriter := tar.NewWriter(buf)

							contents := []byte("---\nimage: some-reconfigured-image\n")

							tarWriter.WriteHeader(&tar.Header{
								Name: "./doesnt-matter",
								Mode: 0644,
								Size: int64(len(contents)),
							})

							tarWriter.Write(contents)
						}

						return buf, nil
					}
				})

				It("is parsed and returned as a Build", func() {
					Ω(fetchedConfig.Image).Should(Equal("some-reconfigured-image"))
				})

				Context("but the output is invalid", func() {
					BeforeEach(func() {
						wardenClient.Connection.WhenStreamingOut = func(handle string, src string) (io.Reader, error) {
							Ω(handle).Should(Equal("some-handle"))

							buf := new(bytes.Buffer)

							if src == "/tmp/resource-destination/some/config/path.yml" {
								tarWriter := tar.NewWriter(buf)

								contents := []byte("[")

								tarWriter.WriteHeader(&tar.Header{
									Name: "./doesnt-matter",
									Mode: 0644,
									Size: int64(len(contents)),
								})

								tarWriter.Write(contents)
							}

							return buf, nil
						}
					})

					It("returns an error", func() {
						Ω(fetchError).Should(HaveOccurred())
					})
				})
			})

			Context("when the config cannot be fetched", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					wardenClient.Connection.WhenStreamingOut = func(handle string, src string) (io.Reader, error) {
						return nil, disaster
					}
				})

				It("returns the error", func() {
					Ω(fetchError).Should(Equal(disaster))
				})
			})

			Context("when the config path does not exist", func() {
				BeforeEach(func() {
					wardenClient.Connection.WhenStreamingOut = func(string, string) (io.Reader, error) {
						return new(bytes.Buffer), nil
					}
				})

				It("returns an error", func() {
					Ω(fetchError).Should(HaveOccurred())
				})
			})
		})

		Context("when streaming in fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				wardenClient.Connection.WhenStreamingIn = func(_, _ string) (io.WriteCloser, error) {
					return nil, disaster
				}
			})

			It("returns the error", func() {
				Ω(fetchError).Should(Equal(disaster))
			})
		})

		Context("when running /tmp/resource/in fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				inError = disaster
			})

			It("returns an err containing stdout/stderr of the process", func() {
				Ω(fetchError).Should(Equal(disaster))
			})
		})

		Context("when /tmp/resource/in exits nonzero", func() {
			BeforeEach(func() {
				inStdout = "some-stdout-data"
				inStderr = "some-stderr-data"
				inExitStatus = 9
			})

			It("returns an err containing stdout/stderr of the process", func() {
				Ω(fetchError).Should(HaveOccurred())
				Ω(fetchError.Error()).Should(ContainSubstring("some-stdout-data"))
				Ω(fetchError.Error()).Should(ContainSubstring("some-stderr-data"))
				Ω(fetchError.Error()).Should(ContainSubstring("exit status 9"))
			})
		})

		Context("when streaming out fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				wardenClient.Connection.WhenStreamingOut = func(_, _ string) (io.Reader, error) {
					return nil, disaster
				}
			})

			It("returns the error", func() {
				Ω(fetchError).Should(Equal(disaster))
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
				go resource.In(input)

				close(abort)

				Eventually(func() interface{} {
					return wardenClient.Connection.Stopped("some-handle")
				}).Should(HaveLen(1))
			})
		})
	})

})
