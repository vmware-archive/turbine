package builder_test

import (
	"errors"
	"log"
	"net/http"

	"github.com/cloudfoundry-incubator/garden/client/connection/fake_connection"
	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/gorilla/websocket"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/ghttp"

	"github.com/winston-ci/prole/api/builds"
	. "github.com/winston-ci/prole/builder"
	"github.com/winston-ci/prole/sourcefetcher/fakesourcefetcher"
)

var _ = Describe("Builder", func() {
	var sourceFetcher *fakesourcefetcher.Fetcher
	var wardenClient *fake_warden_client.FakeClient
	var builder Builder

	var build builds.Build

	primedStream := func(payloads ...warden.ProcessStream) <-chan warden.ProcessStream {
		stream := make(chan warden.ProcessStream, len(payloads))

		for _, payload := range payloads {
			stream <- payload
		}

		close(stream)

		return stream
	}

	BeforeEach(func() {
		sourceFetcher = fakesourcefetcher.New()
		wardenClient = fake_warden_client.New()

		builder = NewBuilder(sourceFetcher, wardenClient)

		build = builds.Build{
			Image: "some-image-name",
			Env: [][2]string{
				{"FOO", "bar"},
				{"BAZ", "buzz"},
			},
			Script: "./bin/test",

			Source: builds.BuildSource{
				Type: "raw",
				URI:  "http://example.com/foo.tar.gz",
				Path: "some/source/path",
			},
		}

		exitStatus := uint32(0)

		successfulStream := primedStream(warden.ProcessStream{
			ExitStatus: &exitStatus,
		})

		wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
			return 42, successfulStream, nil
		}

		wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
			return "some-handle", nil
		}
	})

	It("fetches the build source and copies it in to the container", func() {
		sourceFetcher.FetchResult = "/path/on/disk"

		_, err := builder.Build(build)
		Ω(err).ShouldNot(HaveOccurred())

		Ω(sourceFetcher.Fetched()).Should(ContainElement(build.Source))

		Ω(wardenClient.Connection.CopiedIn("some-handle")).Should(ContainElement(fake_connection.CopyInSpec{
			Source:      "/path/on/disk/",
			Destination: "some/source/path/",
		}))
	})

	It("runs the build's script in the container", func() {
		_, err := builder.Build(build)
		Ω(err).ShouldNot(HaveOccurred())

		Ω(wardenClient.Connection.SpawnedProcesses("some-handle")).Should(ContainElement(warden.ProcessSpec{
			Script: "./bin/test",
			EnvironmentVariables: []warden.EnvironmentVariable{
				{"FOO", "bar"},
				{"BAZ", "buzz"},
			},
		}))
	})

	Context("when a logs url is configured", func() {
		It("emits the build's output via websockets", func() {
			wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
				exitStatus := uint32(0)

				successfulStream := primedStream(
					warden.ProcessStream{
						Source: warden.ProcessStreamSourceStdout,
						Data:   []byte("stdout\n"),
					},
					warden.ProcessStream{
						Source: warden.ProcessStreamSourceStderr,
						Data:   []byte("stderr\n"),
					},
					warden.ProcessStream{
						ExitStatus: &exitStatus,
					},
				)

				return 42, successfulStream, nil
			}

			websocketEndpoint := ghttp.NewServer()

			buf := gbytes.NewBuffer()

			var upgrader = websocket.Upgrader{
				ReadBufferSize:  1024,
				WriteBufferSize: 1024,
				CheckOrigin: func(r *http.Request) bool {
					// allow all connections
					return true
				},
			}

			websocketEndpoint.AppendHandlers(
				func(w http.ResponseWriter, r *http.Request) {
					conn, err := upgrader.Upgrade(w, r, nil)
					if err != nil {
						log.Println(err)
						return
					}

					for {
						_, msg, err := conn.ReadMessage()
						if err != nil {
							break
						}

						buf.Write(msg)
					}
				},
			)

			build.LogsURL = "ws://" + websocketEndpoint.HTTPTestServer.Listener.Addr().String()

			_, err := builder.Build(build)
			Ω(err).ShouldNot(HaveOccurred())

			Eventually(buf).Should(gbytes.Say("creating container from some-image-name...\n"))
			Eventually(buf).Should(gbytes.Say("starting...\n"))
			Eventually(buf).Should(gbytes.Say("stdout\n"))
			Eventually(buf).Should(gbytes.Say("stderr\n"))
		})
	})

	Context("when running the build's script fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
				return 0, nil, disaster
			}
		})

		It("returns true", func() {
			succeeded, err := builder.Build(build)
			Ω(err).Should(Equal(disaster))
			Ω(succeeded).Should(BeFalse())
		})
	})

	Context("when the build's script exits 0", func() {
		BeforeEach(func() {
			wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
				exitStatus := uint32(0)

				return 42, primedStream(warden.ProcessStream{
					ExitStatus: &exitStatus,
				}), nil
			}
		})

		It("returns true", func() {
			succeeded, err := builder.Build(build)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(succeeded).Should(BeTrue())
		})
	})

	Context("when the build's script exits nonzero", func() {
		BeforeEach(func() {
			wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
				exitStatus := uint32(2)

				return 42, primedStream(warden.ProcessStream{
					ExitStatus: &exitStatus,
				}), nil
			}
		})

		It("returns true", func() {
			succeeded, err := builder.Build(build)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(succeeded).Should(BeFalse())
		})
	})

	Context("when creating the container fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			wardenClient.Connection.WhenCreating = func(spec warden.ContainerSpec) (string, error) {
				return "", disaster
			}
		})

		It("returns the error", func() {
			succeeded, err := builder.Build(build)
			Ω(err).Should(Equal(disaster))
			Ω(succeeded).Should(BeFalse())
		})
	})

	Context("when fetching the source fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			sourceFetcher.FetchError = disaster
		})

		It("returns the error", func() {
			succeeded, err := builder.Build(build)
			Ω(err).Should(Equal(disaster))
			Ω(succeeded).Should(BeFalse())
		})
	})

	Context("when copying the source in to the container fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			wardenClient.Connection.WhenCopyingIn = func(handle string, src, dst string) error {
				return disaster
			}
		})

		It("returns the error", func() {
			succeeded, err := builder.Build(build)
			Ω(err).Should(Equal(disaster))
			Ω(succeeded).Should(BeFalse())
		})
	})
})
