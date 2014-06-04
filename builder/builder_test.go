package builder_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"

	"code.google.com/p/go.net/websocket"
	"github.com/cloudfoundry-incubator/garden/client/connection/fake_connection"
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

	Describe("Start", func() {
		var started <-chan RunningBuild
		var errored <-chan error

		var abort chan struct{}

		JustBeforeEach(func() {
			abort = make(chan struct{})
			started, errored = builder.Start(build, abort)
		})

		BeforeEach(func() {
			wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
				return "some-handle", nil
			}

			sourceFetcher.WhenFetching = func(builds.Input, io.Writer, <-chan struct{}) (builds.Config, builds.Version, []builds.MetadataField, io.Reader, error) {
				return builds.Config{}, nil, nil, bytes.NewBufferString("some-data"), nil
			}
		})

		It("creates a container with the specified image", func() {
			Eventually(started).Should(Receive())

			created := wardenClient.Connection.Created()
			Ω(created).Should(HaveLen(1))
			Ω(created[0].RootFSPath).Should(Equal("docker:///some-image-name"))
		})

		Context("when fetching the build's inputs succeeds", func() {
			BeforeEach(func() {
				sourceFetcher.WhenFetching = func(input builds.Input, logs io.Writer, abort <-chan struct{}) (builds.Config, builds.Version, []builds.MetadataField, io.Reader, error) {
					if input.Name == "name1" {
						version := builds.Version{"key": "version-1"}
						metadata := []builds.MetadataField{{Name: "key", Value: "meta-1"}}
						sourceStream := bytes.NewBufferString("some-data-1")
						return builds.Config{}, version, metadata, sourceStream, nil
					}

					if input.Name == "name2" {
						config := builds.Config{Image: "some-reconfigured-image"}
						version := builds.Version{"key": "version-2"}
						metadata := []builds.MetadataField{{Name: "key", Value: "meta-2"}}
						sourceStream := bytes.NewBufferString("some-data-2")
						return config, version, metadata, sourceStream, nil
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

				for _, streamed := range streamedIn {
					switch streamed.Destination {
					case "/tmp/build/src/some/source/path":
						Ω(string(streamed.WriteBuffer.Contents())).Should(Equal("some-data-1"))
						Ω(streamed.WriteBuffer.Closed()).Should(BeTrue())
					case "/tmp/build/src/another/source/path":
						Ω(string(streamed.WriteBuffer.Contents())).Should(Equal("some-data-2"))
						Ω(streamed.WriteBuffer.Closed()).Should(BeTrue())
					default:
						Fail("unknown stream destination: " + streamed.Destination)
					}
				}
			})

			Context("and running the process succeeds", func() {
				BeforeEach(func() {
					wardenClient.Connection.WhenRunning = func(handle string, spec warden.ProcessSpec) (uint32, <-chan warden.ProcessStream, error) {
						return 42, exitedStream(0), nil
					}
				})

				It("notifies that the build is started, with updated inputs (version + metadata)", func() {
					var runningBuild RunningBuild
					Eventually(started).Should(Receive(&runningBuild))

					inputs := runningBuild.Build.Inputs

					Ω(inputs[0].Version).Should(Equal(builds.Version{"key": "version-1"}))
					Ω(inputs[0].Metadata).Should(Equal([]builds.MetadataField{{Name: "key", Value: "meta-1"}}))

					Ω(inputs[1].Version).Should(Equal(builds.Version{"key": "version-2"}))
					Ω(inputs[1].Metadata).Should(Equal([]builds.MetadataField{{Name: "key", Value: "meta-2"}}))
				})

				It("returns the container, container handle, process ID, process stream, and logs", func() {
					var runningBuild RunningBuild
					Eventually(started).Should(Receive(&runningBuild))

					Ω(runningBuild.Container).ShouldNot(BeNil())
					Ω(runningBuild.ContainerHandle).Should(Equal("some-handle"))
					Ω(runningBuild.ProcessID).Should(Equal(uint32(42)))
					Ω(runningBuild.ProcessStream).ShouldNot(BeNil())
					Ω(runningBuild.LogStream).ShouldNot(BeNil())
				})
			})

			Context("and an input reconfigured the build", func() {
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

		Context("when the build is aborted", func() {
			var gotAborts chan (<-chan struct{})

			BeforeEach(func() {
				gotAborts = make(chan (<-chan struct{}), 1)

				sourceFetcher.WhenFetching = func(input builds.Input, logs io.Writer, abort <-chan struct{}) (builds.Config, builds.Version, []builds.MetadataField, io.Reader, error) {
					gotAborts <- abort

					// return abort error to simulate fetching being aborted;
					// assert that the channel closed below
					return builds.Config{}, builds.Version{}, nil, nil, ErrAborted
				}
			})

			It("aborts source fetching", func() {
				Eventually(errored).Should(Receive(Equal(ErrAborted)))

				var abortFetch <-chan struct{}
				Ω(gotAborts).Should(Receive(&abortFetch))

				Ω(abortFetch).ShouldNot(BeClosed())

				close(abort)

				Ω(abortFetch).Should(BeClosed())
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

				Context("and fetching inputs emits logs", func() {
					BeforeEach(func() {
						sourceFetcher.WhenFetching = func(input builds.Input, logs io.Writer, abort <-chan struct{}) (builds.Config, builds.Version, []builds.MetadataField, io.Reader, error) {
							defer GinkgoRecover()

							Ω(logs).ShouldNot(BeNil())
							logs.Write([]byte("hello from source fetcher"))

							return builds.Config{}, nil, nil, bytes.NewBufferString("some-data"), nil
						}
					})

					It("emits them to the sink", func() {
						Eventually(logBuffer).Should(gbytes.Say("hello from source fetcher"))

						var runningBuild RunningBuild
						Eventually(started).Should(Receive(&runningBuild))

						runningBuild.LogStream.Close()
					})
				})
			})

			Context("but the sink disconnects", func() {
				BeforeEach(func() {
					okHandler := websocketSink.GetHandler(0)

					websocketSink.SetHandler(0, func(w http.ResponseWriter, r *http.Request) {
						websocketSink.HTTPTestServer.CloseClientConnections()
					})

					websocketSink.AppendHandlers(okHandler)
				})

				It("retries until it is", func() {
					Eventually(logBuffer, 2).Should(gbytes.Say("starting...\n"))
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
		var succeeded <-chan SucceededBuild
		var failed <-chan error
		var errored <-chan error

		var runningBuild RunningBuild
		var abort chan struct{}

		JustBeforeEach(func() {
			abort = make(chan struct{})
			succeeded, failed, errored = builder.Attach(runningBuild, abort)
		})

		BeforeEach(func() {
			build.Inputs = []builds.Input{
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
			}

			wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
				return "the-attached-container", nil
			}

			container, err := wardenClient.Create(warden.ContainerSpec{})
			Ω(err).ShouldNot(HaveOccurred())

			wardenClient.Connection.WhenCreating = nil

			runningBuild = RunningBuild{
				Build: build,

				ContainerHandle: container.Handle(),
				Container:       container,

				ProcessID: 42,
			}
		})

		Context("when the build's container and process stream are not present", func() {
			BeforeEach(func() {
				runningBuild.Container = nil

				wardenClient.Connection.WhenAttaching = func(handle string, processID uint32) (<-chan warden.ProcessStream, error) {
					defer GinkgoRecover()

					Ω(handle).Should(Equal("the-attached-container"))
					Ω(processID).Should(Equal(uint32(42)))

					return exitedStream(0), nil
				}
			})

			Context("and the container can still be found", func() {
				var lookedUp chan struct{}

				BeforeEach(func() {
					lookedUp = make(chan struct{})

					wardenClient.Connection.WhenListing = func(warden.Properties) ([]string, error) {
						close(lookedUp)
						return []string{runningBuild.ContainerHandle}, nil
					}
				})

				It("looks it up via warden and uses it for attaching", func() {
					Eventually(lookedUp).Should(BeClosed())
					Eventually(succeeded).Should(Receive())
				})
			})

			Context("and the lookup fails", func() {
				BeforeEach(func() {
					wardenClient.Connection.WhenListing = func(warden.Properties) ([]string, error) {
						return []string{}, nil
					}
				})

				It("sends an error result", func() {
					Eventually(errored).Should(Receive())
				})
			})
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
				Eventually(succeeded).Should(Receive())
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

		Context("when the build is aborted", func() {
			BeforeEach(func() {
				runningBuild.ProcessStream = make(chan warden.ProcessStream)
			})

			It("stops the container", func() {
				close(abort)

				Eventually(func() interface{} {
					return wardenClient.Connection.Stopped("the-attached-container")
				}).Should(Equal([]fake_connection.StopSpec{{}}))
			})
		})

		Context("when the build's script exits 0", func() {
			BeforeEach(func() {
				runningBuild.ProcessStream = exitedStream(0)
			})

			It("sends a successful result", func() {
				Eventually(succeeded).Should(Receive())
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

				Context("but the sink disconnects", func() {
					BeforeEach(func() {
						okHandler := websocketSink.GetHandler(0)

						websocketSink.SetHandler(0, func(w http.ResponseWriter, r *http.Request) {
							websocketSink.HTTPTestServer.CloseClientConnections()
						})

						websocketSink.AppendHandlers(okHandler)
					})

					It("retries until it is", func() {
						Eventually(logBuffer, 2).Should(gbytes.Say("stdout\n"))
						Eventually(logBuffer).Should(gbytes.Say("stderr\n"))
					})
				})
			})
		})
	})

	Describe("Complete", func() {
		var finished <-chan builds.Build
		var errored <-chan error
		var succeededBuild SucceededBuild
		var abort chan struct{}

		JustBeforeEach(func() {
			abort = make(chan struct{})
			finished, errored = builder.Complete(succeededBuild, abort)
		})

		BeforeEach(func() {
			build.Inputs = []builds.Input{
				{
					Name:            "name1",
					Type:            "raw",
					DestinationPath: "some/source/path",
					Version:         builds.Version{"key": "in-version-1"},
				},
				{
					Name:            "name2",
					Type:            "raw",
					DestinationPath: "another/source/path",
					Version:         builds.Version{"key": "in-version-2"},
				},
			}

			wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
				return "the-attached-container", nil
			}

			container, err := wardenClient.Create(warden.ContainerSpec{})
			Ω(err).ShouldNot(HaveOccurred())

			wardenClient.Connection.WhenCreating = nil

			succeededBuild = SucceededBuild{
				Build:     build,
				Container: container,
			}
		})

		It("reports inputs as implicit outputs", func() {
			var finishedBuild builds.Build
			Eventually(finished).Should(Receive(&finishedBuild))

			Ω(finishedBuild.Outputs).Should(HaveLen(2))

			Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
				Name:    "name1",
				Type:    "raw",
				Version: builds.Version{"key": "in-version-1"},
			}))

			Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
				Name:    "name2",
				Type:    "raw",
				Version: builds.Version{"key": "in-version-2"},
			}))
		})

		Context("and outputs are configured on the build", func() {
			BeforeEach(func() {
				succeededBuild.Build.Outputs = []builds.Output{
					{
						Name:   "name1",
						Type:   "git",
						Params: builds.Params{"key": "param-1"},
					},
					{
						Name:   "someoutput",
						Type:   "git",
						Params: builds.Params{"key": "param-2"},
					},
				}
			})

			Context("and streaming out succeeds", func() {
				BeforeEach(func() {
					wardenClient.Connection.WhenStreamingOut = func(string, string) (io.Reader, error) {
						return bytes.NewBufferString("streamed-out"), nil
					}

					sync := make(chan bool)

					outputter.WhenPerformingOutput = func(output builds.Output, src io.Reader, logs io.Writer, abort <-chan struct{}) (builds.Version, []builds.MetadataField, error) {
						if output.Params["key"] == "param-1" {
							<-sync
							version := builds.Version{"key": "out-version-1"}
							metadata := []builds.MetadataField{{Name: "name", Value: "out-meta-1"}}
							return version, metadata, nil
						}

						// Implicit output created for an input 'name2'
						if len(output.Params) == 0 {
							version := builds.Version{"key": "in-version-2"}
							metadata := []builds.MetadataField{{Name: "name", Value: "out-meta-2"}}
							return version, metadata, nil
						}

						if output.Params["key"] == "param-2" {
							close(sync)
							version := builds.Version{"key": "out-version-3"}
							metadata := []builds.MetadataField{{Name: "name", Value: "out-meta-3"}}
							return version, metadata, nil
						}

						panic("unknown output")
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

					Ω(outputs).Should(ContainElement(succeededBuild.Build.Outputs[0]))
					Ω(outputs).Should(ContainElement(succeededBuild.Build.Outputs[1]))
				})

				It("reports the outputs", func() {
					var finishedBuild builds.Build
					Eventually(finished).Should(Receive(&finishedBuild))

					Ω(finishedBuild.Outputs).Should(HaveLen(3))

					Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
						Name:     "name1",
						Type:     "git",
						Params:   builds.Params{"key": "param-1"},
						Version:  builds.Version{"key": "out-version-1"},
						Metadata: []builds.MetadataField{{Name: "name", Value: "out-meta-1"}},
					}))

					// Implicit output created for an input 'name2'
					Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
						Name:     "name2",
						Type:     "raw",
						Params:   nil,
						Version:  builds.Version{"key": "in-version-2"},
						Metadata: nil,
					}))

					Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
						Name:     "someoutput",
						Type:     "git",
						Params:   builds.Params{"key": "param-2"},
						Version:  builds.Version{"key": "out-version-3"},
						Metadata: []builds.MetadataField{{Name: "name", Value: "out-meta-3"}},
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

			Context("when the build is aborted", func() {
				var gotAborts chan (<-chan struct{})

				BeforeEach(func() {
					gotAborts = make(chan (<-chan struct{}), len(succeededBuild.Build.Outputs))

					outputter.WhenPerformingOutput = func(
						output builds.Output,
						tarStream io.Reader,
						logs io.Writer,
						abort <-chan struct{},
					) (builds.Version, []builds.MetadataField, error) {
						gotAborts <- abort

						// return abort error to simulate fetching being aborted;
						// assert that the channel closed below
						return builds.Version{}, nil, ErrAborted
					}
				})

				It("aborts performing the outputs", func() {
					Eventually(errored).Should(Receive(Equal(ErrAborted)))

					var abortFetch <-chan struct{}
					Ω(gotAborts).Should(Receive(&abortFetch))

					Ω(abortFetch).ShouldNot(BeClosed())

					close(abort)

					Ω(abortFetch).Should(BeClosed())
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

			Describe("logs emitted by output", func() {
				var logBuffer *gbytes.Buffer

				BeforeEach(func() {
					logBuffer = gbytes.NewBuffer()

					outputter.WhenPerformingOutput = func(output builds.Output, src io.Reader, logs io.Writer, abort <-chan struct{}) (builds.Version, []builds.MetadataField, error) {
						defer GinkgoRecover()

						Ω(logs).ShouldNot(BeNil())
						logs.Write([]byte("hello from outputter"))

						return nil, nil, nil
					}
				})

				Context("when the running build already has a log stream", func() {
					BeforeEach(func() {
						succeededBuild.LogStream = logBuffer
					})

					It("emits the build's output to it", func() {
						Eventually(logBuffer).Should(gbytes.Say("hello from outputter"))
					})
				})

				Context("when a logs url is configured", func() {
					var websocketSink *ghttp.Server

					BeforeEach(func() {
						succeededBuild.Build.LogsURL, websocketSink = websocketListener(logBuffer)
					})

					It("emits the build's output via websockets", func() {
						Eventually(logBuffer).Should(gbytes.Say("hello from outputter"))
					})

					Context("but the sink disconnects", func() {
						BeforeEach(func() {
							okHandler := websocketSink.GetHandler(0)

							websocketSink.SetHandler(0, func(w http.ResponseWriter, r *http.Request) {
								websocketSink.HTTPTestServer.CloseClientConnections()
							})

							websocketSink.AppendHandlers(okHandler)
						})

						It("retries until it is", func() {
							Eventually(logBuffer, 2).Should(gbytes.Say("hello from outputter"))
						})
					})
				})
			})
		})
	})
})
