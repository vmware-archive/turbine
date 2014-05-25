package builder_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"

	"code.google.com/p/go.net/websocket"
	"github.com/cloudfoundry-incubator/garden/client/fake_warden_client"
	"github.com/cloudfoundry-incubator/garden/warden"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/ghttp"

	"github.com/winston-ci/prole/api/builds"
	. "github.com/winston-ci/prole/builder"
	"github.com/winston-ci/prole/outputter/fakeoutputter"
	"github.com/winston-ci/prole/sourcefetcher/fakesourcefetcher"
)

var _ = Describe("Builder", func() {
	var sourceFetcher *fakesourcefetcher.Fetcher
	var outputter *fakeoutputter.Outputter
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

	exitedStream := func(exitStatus uint32) <-chan warden.ProcessStream {
		return primedStream(warden.ProcessStream{
			ExitStatus: &exitStatus,
		})
	}

	websocketListener := func(buf io.WriteCloser) (string, *ghttp.Server) {
		websocketEndpoint := ghttp.NewServer()

		websocketEndpoint.AppendHandlers(
			func(w http.ResponseWriter, r *http.Request) {
				websocket.Server{Handler: func(conn *websocket.Conn) {
					_, err := io.Copy(buf, conn)
					Ω(err).ShouldNot(HaveOccurred())

					buf.Close()
				}}.ServeHTTP(w, r)
			},
		)

		addr := websocketEndpoint.HTTPTestServer.Listener.Addr().String()

		return "ws://" + addr, websocketEndpoint
	}

	BeforeEach(func() {
		sourceFetcher = fakesourcefetcher.New()
		outputter = fakeoutputter.New()
		wardenClient = fake_warden_client.New()
		builder = NewBuilder(sourceFetcher, outputter, wardenClient)

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
					Name:            "name1",
					Type:            "raw",
					DestinationPath: "some/source/path",
				},
				{
					Name:            "name2",
					Type:            "raw",
					DestinationPath: "another/source/path",
				},
			},
		}
	})

	Describe("Build", func() {
		var started <-chan RunningBuild
		var errored <-chan error

		JustBeforeEach(func() {
			started, errored = builder.Build(build)
		})

		BeforeEach(func() {

			wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
				return "some-handle", nil
			}

			sourceFetcher.WhenFetching = func(builds.Input) (builds.Config, builds.Source, io.Reader, error) {
				return builds.Config{}, nil, bytes.NewBufferString("some-data"), nil
			}
		})

		It("creates a container with the specified image", func() {
			Eventually(started).Should(Receive())

			created := wardenClient.Connection.Created()
			Ω(created).Should(HaveLen(1))
			Ω(created[0].RootFSPath).Should(Equal("docker:///some-image-name"))
		})

		Context("when fetching the build's sources succeeds", func() {
			BeforeEach(func() {
				source1 := builds.Source("some-source-1")
				source2 := builds.Source("some-source-2")

				sourceStream1 := bytes.NewBufferString("some-data-1")
				sourceStream2 := bytes.NewBufferString("some-data-2")

				sourceFetcher.WhenFetching = func(input builds.Input) (builds.Config, builds.Source, io.Reader, error) {
					if input.Name == "name1" {
						return builds.Config{}, source1, sourceStream1, nil
					}

					if input.Name == "name2" {
						config := builds.Config{Image: "some-reconfigured-image"}
						return config, source2, sourceStream2, nil
					}

					panic("unknown stream")
				}
			})

			It("streams them in to the container", func() {
				Eventually(started).Should(Receive())

				Ω(sourceFetcher.Fetched()).Should(Equal([]builds.Input{
					{
						Name:            "name1",
						Type:            "raw",
						DestinationPath: "some/source/path",
					},
					{
						Name:            "name2",
						Type:            "raw",
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

			Context("and running the process succeeds", func() {
				BeforeEach(func() {
					wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
						return 42, exitedStream(0), nil
					}
				})

				It("notifies that the build is running started, with updated sources and config", func() {
					var runningBuild RunningBuild
					Eventually(started).Should(Receive(&runningBuild))

					Ω(runningBuild.Build.Inputs[0].Source).Should(Equal(builds.Source("some-source-1")))
					Ω(runningBuild.Build.Inputs[1].Source).Should(Equal(builds.Source("some-source-2")))
				})
			})

			Context("and a source reconfigured the build", func() {
				BeforeEach(func() {
					build.Inputs[1].ConfigPath = "some/config/path.yml"
				})

				It("sends the reconfigured build as the started build", func() {
					var startedBuild RunningBuild
					Eventually(started).Should(Receive(&startedBuild))

					Ω(startedBuild.Build.Config.Image).Should(Equal("some-reconfigured-image"))
				})
			})
		})

		It("runs the build's script in the container", func() {
			Eventually(started).Should(Receive())

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
				Eventually(started).Should(Receive())

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
			var websocketSink *ghttp.Server

			BeforeEach(func() {
				logBuffer = gbytes.NewBuffer()

				build.LogsURL, websocketSink = websocketListener(logBuffer)
			})

			Context("and the sink is listening", func() {
				AfterEach(func() {
					websocketSink.Close()
				})

				It("emits the build's output via websockets", func() {
					Eventually(logBuffer).Should(gbytes.Say("creating container from some-image-name...\n"))
					Eventually(logBuffer).Should(gbytes.Say("starting...\n"))

					var runningBuild RunningBuild
					Eventually(started).Should(Receive(&runningBuild))

					runningBuild.LogStream.Close()
				})
			})

			Context("but the sink is not listening", func() {
				BeforeEach(func() {
					websocketSink.Close()
				})

				It("sends the error result", func() {
					Eventually(errored).Should(Receive())
				})
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

	Describe("Attach", func() {
		var finished <-chan builds.Build
		var failed <-chan error
		var errored <-chan error
		var runningBuild RunningBuild

		JustBeforeEach(func() {
			finished, failed, errored = builder.Attach(runningBuild)
		})

		BeforeEach(func() {
			build.Inputs = []builds.Input{
				{
					Name:            "name1",
					Type:            "raw",
					DestinationPath: "some/source/path",
					Source:          builds.Source("some-source-1"),
				},
				{
					Name:            "name2",
					Type:            "raw",
					DestinationPath: "another/source/path",
					Source:          builds.Source("some-source-2"),
				},
			}

			wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
				return "the-attached-container", nil
			}

			container, err := wardenClient.Create(warden.ContainerSpec{})
			Ω(err).ShouldNot(HaveOccurred())

			wardenClient.Connection.WhenCreating = nil

			runningBuild = RunningBuild{
				Build:     build,
				Container: container,

				ProcessID: 42,
			}
		})

		Context("when the build's process stream is not present", func() {
			var attached chan struct{}

			BeforeEach(func() {
				attached = make(chan struct{})

				runningBuild.ProcessStream = nil

				wardenClient.Connection.WhenAttaching = func(handle string, processID uint32) (<-chan warden.ProcessStream, error) {
					defer GinkgoRecover()

					Ω(handle).Should(Equal("the-attached-container"))
					Ω(processID).Should(Equal(uint32(42)))

					close(attached)

					return exitedStream(0), nil
				}
			})

			It("attaches to the build's process", func() {
				Eventually(attached).Should(BeClosed())
				Eventually(finished).Should(Receive())
			})

			Context("and attaching fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					wardenClient.Connection.WhenAttaching = func(handle string, processID uint32) (<-chan warden.ProcessStream, error) {
						return nil, disaster
					}
				})

				It("sends the error result", func() {
					Eventually(errored).Should(Receive(Equal(disaster)))
				})
			})
		})

		Context("when the build's script exits 0", func() {
			BeforeEach(func() {
				runningBuild.ProcessStream = exitedStream(0)
			})

			It("sends a successful result", func() {
				Eventually(finished).Should(Receive())
			})

			It("reports regular inputs as output sources", func() {
				var finishedBuild builds.Build
				Eventually(finished).Should(Receive(&finishedBuild))

				Ω(finishedBuild.Outputs).Should(HaveLen(2))

				Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
					Name:   "name1",
					Type:   "raw",
					Source: builds.Source("some-source-1"),
				}))

				Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
					Name:   "name2",
					Type:   "raw",
					Source: builds.Source("some-source-2"),
				}))
			})

			Context("and outputs are configured on the build", func() {
				BeforeEach(func() {
					runningBuild.Build.Outputs = []builds.Output{
						{
							Name:   "name1",
							Type:   "git",
							Params: builds.Params("123"),
						},
						{
							Name:   "someoutput",
							Type:   "git",
							Params: builds.Params("456"),
						},
					}
				})

				Context("and streaming out succeeds", func() {
					BeforeEach(func() {
						wardenClient.Connection.WhenStreamingOut = func(string, string) (io.Reader, error) {
							return bytes.NewBufferString("streamed-out"), nil
						}

						sync := make(chan bool)

						outputter.WhenPerformingOutput = func(output builds.Output, src io.Reader) (builds.Source, error) {
							if string(output.Params) == "123" {
								<-sync
								return builds.Source("output-1"), nil
							} else {
								close(sync)
								return builds.Source("output-2"), nil
							}
						}
					})

					It("evaluates every output in parallel with the source and params", func() {
						Eventually(finished).Should(Receive())

						performedOutputs := outputter.PerformedOutputs()
						Ω(performedOutputs).Should(HaveLen(2))

						outputs := []builds.Output{}
						for _, performed := range performedOutputs {
							outputs = append(outputs, performed.Output)

							Ω(string(performed.StreamedIn.Contents())).Should(Equal("streamed-out"))
							Ω(performed.StreamedIn.Closed()).Should(BeTrue())
						}

						Ω(outputs).Should(ContainElement(runningBuild.Build.Outputs[0]))
						Ω(outputs).Should(ContainElement(runningBuild.Build.Outputs[1]))
					})

					It("reports the output sources", func() {
						var finishedBuild builds.Build
						Eventually(finished).Should(Receive(&finishedBuild))

						Ω(finishedBuild.Outputs).Should(HaveLen(3))

						Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
							Name:   "name1",
							Type:   "git",
							Params: builds.Params("123"),
							Source: builds.Source("output-1"),
						}))

						Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
							Name:   "name2",
							Type:   "raw",
							Source: builds.Source("some-source-2"),
						}))

						Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
							Name:   "someoutput",
							Type:   "git",
							Params: builds.Params("456"),
							Source: builds.Source("output-2"),
						}))
					})

					Context("and an output fails", func() {
						disaster := errors.New("oh no!")

						BeforeEach(func() {
							outputter.PerformOutputError = disaster
						})

						It("sends the error result", func() {
							Eventually(errored).Should(Receive(Equal(disaster)))
						})
					})
				})

				Context("and streaming out fails", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						wardenClient.Connection.WhenStreamingOut = func(string, string) (io.Reader, error) {
							return nil, disaster
						}
					})

					It("sends the error result", func() {
						Eventually(errored).Should(Receive(Equal(disaster)))
					})
				})
			})
		})

		Context("when the build's script exits nonzero", func() {
			BeforeEach(func() {
				runningBuild.ProcessStream = exitedStream(2)
			})

			It("sends a failed result", func() {
				Eventually(failed).Should(Receive())
			})
		})

		Context("when the process outputs", func() {
			var logBuffer *gbytes.Buffer

			BeforeEach(func() {
				logBuffer = gbytes.NewBuffer()

				exitStatus := uint32(0)
				runningBuild.ProcessStream = primedStream(
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
			})

			Context("and the running build already has a log stream", func() {
				BeforeEach(func() {
					runningBuild.LogStream = logBuffer
				})

				It("emits the build's output to it", func() {
					Eventually(logBuffer).Should(gbytes.Say("stdout\n"))
					Eventually(logBuffer).Should(gbytes.Say("stderr\n"))
				})
			})

			Context("and a logs url is configured", func() {
				var websocketSink *ghttp.Server

				BeforeEach(func() {
					runningBuild.Build.LogsURL, websocketSink = websocketListener(logBuffer)
				})

				It("emits the build's output via websockets", func() {
					Eventually(logBuffer).Should(gbytes.Say("stdout\n"))
					Eventually(logBuffer).Should(gbytes.Say("stderr\n"))
				})

				Context("but the sink is not listening", func() {
					BeforeEach(func() {
						websocketSink.Close()
					})

					It("sends the error result", func() {
						Eventually(errored).Should(Receive())
					})
				})
			})
		})
	})
})
