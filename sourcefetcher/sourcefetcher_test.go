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

		payload []byte
	)

	buildSource := builds.BuildSource{
		Type: "some-resource",
	}

	BeforeEach(func() {
		resourceTypes = config.ResourceTypes{}
		wardenClient = fake_warden_client.New()

		payload = []byte("some-payload")

		wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
			return "some-handle", nil
		}

		wardenClient.Connection.WhenRunning = func(string, warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
			stream := make(chan warden.ProcessStream, 1)

			exitStatus := uint32(0)
			stream <- warden.ProcessStream{
				ExitStatus: &exitStatus,
			}

			return 0, stream, nil
		}
	})

	JustBeforeEach(func() {
		sourceFetcher = NewSourceFetcher(resourceTypes, wardenClient)
	})

	Context("when the source's resource type is configured", func() {
		BeforeEach(func() {
			resourceTypes = append(resourceTypes, config.ResourceType{
				Name:  "some-resource",
				Image: "some-resource-image",
			})
		})

		It("creates a container with the image configured via the source's type", func() {
			_, _, err := sourceFetcher.Fetch(buildSource, payload)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(wardenClient.Connection.Created()).Should(Equal([]warden.ContainerSpec{
				{
					RootFSPath: "image:some-resource-image",
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

			_, _, err := sourceFetcher.Fetch(buildSource, payload)
			Ω(err).ShouldNot(HaveOccurred())

			tarReader := tar.NewReader(bytes.NewBuffer(buffer.Contents()))

			hdr, err := tarReader.Next()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(hdr.Name).Should(Equal("./input.json"))
			Ω(hdr.Mode).Should(Equal(int64(0644)))

			inputConfig, err := ioutil.ReadAll(tarReader)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(string(inputConfig)).Should(Equal("some-payload"))

			_, err = tarReader.Next()
			Ω(err).Should(Equal(io.EOF))

			Ω(buffer.Closed()).Should(BeTrue())
		})

		It("runs /tmp/resource/in <path> with the contents of the input config file on stdin", func() {
			_, _, err := sourceFetcher.Fetch(buildSource, payload)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(wardenClient.Connection.SpawnedProcesses("some-handle")).Should(Equal([]warden.ProcessSpec{
				{
					Script: "/tmp/resource/in /tmp/resource-destination < /tmp/resource-artifacts/input.json",
				},
			}))
		})

		It("returns the output stream of /tmp/resource-destination/", func() {
			wardenClient.Connection.WhenStreamingOut = func(handle string, source string) (io.Reader, error) {
				Ω(handle).Should(Equal("some-handle"))

				streamOut := new(bytes.Buffer)

				if source == "/tmp/resource-destination/" {
					streamOut.WriteString("sup")
				}

				return streamOut, nil
			}

			_, fetchedStream, err := sourceFetcher.Fetch(buildSource, payload)
			Ω(err).ShouldNot(HaveOccurred())

			contents, err := ioutil.ReadAll(fetchedStream)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(string(contents)).Should(Equal("sup"))
		})

		Context("when a config path is specified", func() {
			BeforeEach(func() {
				buildSource.ConfigPath = "some/config/path.yml"
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
					config, _, err := sourceFetcher.Fetch(buildSource, payload)
					Ω(err).ShouldNot(HaveOccurred())

					Ω(config.Image).Should(Equal("some-reconfigured-image"))
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
						_, _, err := sourceFetcher.Fetch(buildSource, payload)
						Ω(err).Should(HaveOccurred())
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
					_, _, err := sourceFetcher.Fetch(buildSource, payload)
					Ω(err).Should(Equal(disaster))
				})
			})

			Context("when the config path does not exist", func() {
				BeforeEach(func() {
					wardenClient.Connection.WhenStreamingOut = func(string, string) (io.Reader, error) {
						return new(bytes.Buffer), nil
					}
				})

				It("returns an error", func() {
					_, _, err := sourceFetcher.Fetch(buildSource, payload)
					Ω(err).Should(HaveOccurred())
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
					_, _, err := sourceFetcher.Fetch(buildSource, payload)
					Ω(err).Should(Equal(disaster))
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
					_, _, err := sourceFetcher.Fetch(buildSource, payload)
					Ω(err).Should(Equal(disaster))
				})
			})

			Context("when running /tmp/resource/in fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					wardenClient.Connection.WhenRunning = func(string, warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
						return 0, nil, disaster
					}
				})

				It("returns an err containing stdout/stderr of the process", func() {
					_, _, err := sourceFetcher.Fetch(buildSource, payload)
					Ω(err).Should(Equal(disaster))
				})
			})

			Context("when /tmp/resource/in fails", func() {
				BeforeEach(func() {
					wardenClient.Connection.WhenRunning = func(string, warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
						stream := make(chan warden.ProcessStream, 3)

						stream <- warden.ProcessStream{
							Source: warden.ProcessStreamSourceStdout,
							Data:   []byte("some-stdout-data"),
						}

						stream <- warden.ProcessStream{
							Source: warden.ProcessStreamSourceStderr,
							Data:   []byte("some-stderr-data"),
						}

						failedExitStatus := uint32(9)
						stream <- warden.ProcessStream{
							ExitStatus: &failedExitStatus,
						}

						return 0, stream, nil
					}
				})

				It("returns an err containing stdout/stderr of the process", func() {
					_, _, err := sourceFetcher.Fetch(buildSource, payload)
					Ω(err).Should(HaveOccurred())
					Ω(err.Error()).Should(ContainSubstring("some-stdout-data"))
					Ω(err.Error()).Should(ContainSubstring("some-stderr-data"))
					Ω(err.Error()).Should(ContainSubstring("exit status 9"))
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
					_, _, err := sourceFetcher.Fetch(buildSource, payload)
					Ω(err).Should(Equal(disaster))
				})
			})
		})
	})

	Context("when the source's resource type is unknown", func() {
		It("returns ErrUnknownSourceType", func() {
			_, _, err := sourceFetcher.Fetch(builds.BuildSource{
				Type: "lol-butts",
			}, payload)
			Ω(err).Should(Equal(ErrUnknownSourceType))
		})

		It("does not create a container", func() {
			_, _, err := sourceFetcher.Fetch(builds.BuildSource{
				Type: "lol-butts",
			}, payload)
			Ω(err).Should(HaveOccurred())

			Ω(wardenClient.Connection.Created()).Should(BeEmpty())
		})
	})
})
