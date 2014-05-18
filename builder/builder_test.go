package builder_test

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"

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

	var started <-chan builds.Build
	var finished <-chan bool
	var errored <-chan error
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
			Config: builds.Config{
				Image: "some-image-name",

				Env: [][2]string{
					{"FOO", "bar"},
					{"BAZ", "buzz"},
				},
				Script: "./bin/test",
			},

			Inputs: []builds.Input{
				{
					Type: "raw",

					DestinationPath: "some/source/path",
				},
				{
					Type: "raw",

					DestinationPath: "another/source/path",
				},
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

		sourceFetcher.WhenFetching = func(builds.Input) (builds.Config, io.Reader, error) {
			return builds.Config{}, bytes.NewBufferString("some-data"), nil
		}
	})

	JustBeforeEach(func() {
		started, finished, errored = builder.Build(build)
	})

	It("creates a container with the specified image", func() {
		Eventually(finished).Should(Receive())

		created := wardenClient.Connection.Created()
		Ω(created).Should(HaveLen(1))
		Ω(created[0].RootFSPath).Should(Equal("image:some-image-name"))
	})

	Context("when fetching the build's sources succeeds", func() {
		BeforeEach(func() {
			sourceStream1 := bytes.NewBufferString("some-data-1")
			sourceStream2 := bytes.NewBufferString("some-data-2")

			sourceFetcher.WhenFetching = func(input builds.Input) (builds.Config, io.Reader, error) {
				if input.DestinationPath == "some/source/path" {
					return builds.Config{}, sourceStream1, nil
				}
				if input.DestinationPath == "another/source/path" {
					return builds.Config{}, sourceStream2, nil
				}
				panic("unknown stream")
			}
		})

		It("streams them in to the container", func() {
			Eventually(finished).Should(Receive())

			Ω(sourceFetcher.Fetched()).Should(Equal([]builds.Input{
				{
					Type:            "raw",
					ConfigPath:      "",
					DestinationPath: "some/source/path",
				},
				{
					Type:            "raw",
					ConfigPath:      "",
					DestinationPath: "another/source/path",
				},
			}))

			streamedIn := wardenClient.Connection.StreamedIn("some-handle")
			Ω(streamedIn).Should(HaveLen(2))

			Ω(streamedIn[0].Destination).Should(Equal("/tmp/build/src/some/source/path"))
			Ω(string(streamedIn[0].WriteBuffer.Contents())).Should(Equal("some-data-1"))
			Ω(streamedIn[0].WriteBuffer.Closed()).Should(BeTrue())

			Ω(streamedIn[1].Destination).Should(Equal("/tmp/build/src/another/source/path"))
			Ω(string(streamedIn[1].WriteBuffer.Contents())).Should(Equal("some-data-2"))
			Ω(streamedIn[1].WriteBuffer.Closed()).Should(BeTrue())
		})
	})

	It("runs the build's script in the container", func() {
		Eventually(finished).Should(Receive())

		Ω(wardenClient.Connection.SpawnedProcesses("some-handle")).Should(ContainElement(warden.ProcessSpec{
			Script: `cd /tmp/build/src
./bin/test`,
			EnvironmentVariables: []warden.EnvironmentVariable{
				{"FOO", "bar"},
				{"BAZ", "buzz"},
			},
		}))
	})

	Context("when privileged is true", func() {
		BeforeEach(func() {
			build.Privileged = true
		})

		It("runs the build privileged", func() {
			Eventually(finished).Should(Receive())

			Ω(wardenClient.Connection.SpawnedProcesses("some-handle")).Should(ContainElement(warden.ProcessSpec{
				Script: `cd /tmp/build/src
./bin/test`,
				Privileged: true,
				EnvironmentVariables: []warden.EnvironmentVariable{
					{"FOO", "bar"},
					{"BAZ", "buzz"},
				},
			}))
		})
	})

	Context("when a logs url is configured", func() {
		var logBuffer *gbytes.Buffer

		BeforeEach(func() {
			logBuffer = gbytes.NewBuffer()

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
						if err == io.EOF {
							break
						}

						Ω(err).ShouldNot(HaveOccurred())

						logBuffer.Write(msg)
					}

					logBuffer.Close()
				},
			)

			build.LogsURL = "ws://" + websocketEndpoint.HTTPTestServer.Listener.Addr().String()
		})

		It("emits the build's output via websockets", func() {
			Eventually(logBuffer).Should(gbytes.Say("creating container from some-image-name...\n"))
			Eventually(logBuffer).Should(gbytes.Say("starting...\n"))
			Eventually(logBuffer).Should(gbytes.Say("stdout\n"))
			Eventually(logBuffer).Should(gbytes.Say("stderr\n"))
		})
	})

	Context("when running the build's script fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
				return 0, nil, disaster
			}
		})

		It("sends the error result", func() {
			Eventually(errored).Should(Receive(Equal(disaster)))
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

		It("sends a successful result", func() {
			Eventually(finished).Should(Receive(BeTrue()))
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

		It("sends a failed result", func() {
			Eventually(finished).Should(Receive(BeFalse()))
		})
	})

	Context("when creating the container fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			wardenClient.Connection.WhenCreating = func(spec warden.ContainerSpec) (string, error) {
				return "", disaster
			}
		})

		It("sends the error result", func() {
			Eventually(errored).Should(Receive(Equal(disaster)))
		})
	})

	Context("when fetching the source fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			sourceFetcher.FetchError = disaster
		})

		It("sends the error result", func() {
			Eventually(errored).Should(Receive(Equal(disaster)))
		})
	})

	Context("when copying the source in to the container fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			wardenClient.Connection.WhenStreamingIn = func(handle string, dst string) (io.WriteCloser, error) {
				return nil, disaster
			}
		})

		It("sends the error result", func() {
			Eventually(errored).Should(Receive(Equal(disaster)))
		})
	})
})
