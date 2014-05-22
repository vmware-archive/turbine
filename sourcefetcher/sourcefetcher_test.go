package sourcefetcher_test

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
	. "github.com/winston-ci/prole/sourcefetcher"
)

var _ = Describe("SourceFetcher", func() {
	var (
		resourceTypes config.ResourceTypes
		wardenClient  *fake_warden_client.FakeClient
		sourceFetcher *SourceFetcher

		input builds.Input

		inStdout     string
		inStderr     string
		inExitStatus uint32
		inError      error

		extractedConfig builds.Config
		fetchedSource   builds.Source
		fetchedStream   io.Reader
		fetchError      error
	)

	BeforeEach(func() {
		resourceTypes = config.ResourceTypes{}
		wardenClient = fake_warden_client.New()

		input = builds.Input{
			Type:   "some-resource",
			Source: builds.Source("some-source"),
		}

		wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
			return "some-handle", nil
		}

		inStdout = "[]"
		inStderr = ""
		inExitStatus = 0
		inError = nil
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

		sourceFetcher = NewSourceFetcher(resourceTypes, wardenClient)

		extractedConfig, fetchedSource, fetchedStream, fetchError = sourceFetcher.Fetch(input)
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

		Context("when streaming succeeds", func() {
			var streamedIn *gbytes.Buffer

			BeforeEach(func() {
				streamedIn = gbytes.NewBuffer()

				wardenClient.Connection.WhenStreamingIn = func(handle string, destination string) (io.WriteCloser, error) {
					Ω(handle).Should(Equal("some-handle"))
					Ω(destination).Should(Equal("/tmp/resource-artifacts/"))
					return streamedIn, nil
				}
			})

			It("creates a file with the input configuration", func() {
				Ω(fetchError).ShouldNot(HaveOccurred())

				tarReader := tar.NewReader(bytes.NewBuffer(streamedIn.Contents()))

				hdr, err := tarReader.Next()
				Ω(err).ShouldNot(HaveOccurred())
				Ω(hdr.Name).Should(Equal("./input.json"))
				Ω(hdr.Mode).Should(Equal(int64(0644)))

				inputConfig, err := ioutil.ReadAll(tarReader)
				Ω(err).ShouldNot(HaveOccurred())

				Ω(string(inputConfig)).Should(Equal("some-source"))

				_, err = tarReader.Next()
				Ω(err).Should(Equal(io.EOF))

				Ω(streamedIn.Closed()).Should(BeTrue())
			})
		})

		It("runs /tmp/resource/in <path> with the contents of the input config file on stdin", func() {
			Ω(fetchError).ShouldNot(HaveOccurred())

			Ω(wardenClient.Connection.SpawnedProcesses("some-handle")).Should(Equal([]warden.ProcessSpec{
				{
					Script: "/tmp/resource/in /tmp/resource-destination < /tmp/resource-artifacts/input.json",
				},
			}))
		})

		Context("when /tmp/resource/in prints the source", func() {
			BeforeEach(func() {
				inStdout = "some-new-source"
			})

			It("returns the build source printed out by /tmp/resource/in", func() {
				Ω(fetchedSource).Should(Equal(builds.Source("some-new-source")))
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
					Ω(extractedConfig.Image).Should(Equal("some-reconfigured-image"))
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

		Context("when creating the container fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
					return "", disaster
				}
			})

			It("returns the error", func() {
				Ω(fetchError).Should(Equal(disaster))
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
	})

	Context("when the source's resource type is unknown", func() {
		BeforeEach(func() {
			input.Type = "lol-butts"
		})

		It("returns ErrUnknownSourceType", func() {
			Ω(fetchError).Should(Equal(ErrUnknownSourceType))
		})

		It("does not create a container", func() {
			Ω(wardenClient.Connection.Created()).Should(BeEmpty())
		})
	})
})
