package builder_test

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
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
	"github.com/winston-ci/prole/resource"
	resourcefakes "github.com/winston-ci/prole/resource/fakes"
)

var _ = Describe("Builder", func() {
	var tracker *resourcefakes.FakeTracker
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
		tracker = new(resourcefakes.FakeTracker)
		wardenClient = fake_warden_client.New()
		builder = NewBuilder(tracker, wardenClient)

		build = builds.Build{
			Config: builds.Config{
				Image: "some-image-name",

				Env: []map[string]string{
					{"FOO": "bar"},
					{"BAZ": "buzz"},
				},
				Script: "./bin/test",
			},

			Inputs: []builds.Input{
				{
					Name:            "first-resource",
					Type:            "raw",
					DestinationPath: "some/source/path",
				},
				{
					Name:            "second-resource",
					Type:            "raw",
					DestinationPath: "another/source/path",
				},
			},
		}
	})

	Describe("Start", func() {
		var started <-chan RunningBuild
		var errored <-chan error

		var resource1 *resourcefakes.FakeResource
		var resource2 *resourcefakes.FakeResource

		BeforeEach(func() {
			resource1 = new(resourcefakes.FakeResource)
			resource2 = new(resourcefakes.FakeResource)

			resources := make(chan resource.Resource, 2)
			resources <- resource1
			resources <- resource2

			tracker.InitStub = func(typ string, logs io.Writer, abort <-chan struct{}) (resource.Resource, error) {
				return <-resources, nil
			}

			wardenClient.Connection.WhenCreating = func(warden.ContainerSpec) (string, error) {
				return "some-handle", nil
			}
		})

		var abort chan struct{}

		JustBeforeEach(func() {
			abort = make(chan struct{})
			started, errored = builder.Start(build, abort)
		})

		Context("when fetching the build's inputs succeeds", func() {
			BeforeEach(func() {
				resource1.InStub = func(input builds.Input) (io.Reader, builds.Input, builds.Config, error) {
					sourceStream := bytes.NewBufferString("some-data-1")
					input.Version = builds.Version{"key": "version-1"}
					input.Metadata = []builds.MetadataField{{Name: "key", Value: "meta-1"}}
					return sourceStream, input, builds.Config{}, nil
				}

				resource2.InStub = func(input builds.Input) (io.Reader, builds.Input, builds.Config, error) {
					sourceStream := bytes.NewBufferString("some-data-2")
					config := builds.Config{Image: "some-reconfigured-image"}
					input.Version = builds.Version{"key": "version-2"}
					input.Metadata = []builds.MetadataField{{Name: "key", Value: "meta-2"}}
					return sourceStream, input, config, nil
				}
			})

			It("creates a container with the specified image", func() {
				Eventually(started).Should(Receive())

				created := wardenClient.Connection.Created()
				Ω(created).Should(HaveLen(1))
				Ω(created[0].RootFSPath).Should(Equal("docker:///some-image-name"))
			})

			It("streams them in to the container", func() {
				Eventually(started).Should(Receive())

				Ω(resource1.InCallCount()).Should(Equal(1))
				Ω(resource1.InArgsForCall(0)).Should(Equal(builds.Input{
					Name:            "first-resource",
					Type:            "raw",
					DestinationPath: "some/source/path",
				}))

				Ω(resource2.InCallCount()).Should(Equal(1))
				Ω(resource2.InArgsForCall(0)).Should(Equal(builds.Input{
					Name:            "second-resource",
					Type:            "raw",
					DestinationPath: "another/source/path",
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

			It("releases each resource", func() {
				Eventually(started).Should(Receive())

				Ω(tracker.ReleaseCallCount()).Should(Equal(2))

				allReleased := []resource.Resource{
					tracker.ReleaseArgsForCall(0),
					tracker.ReleaseArgsForCall(1),
				}

				Ω(allReleased).Should(ContainElement(resource1))
				Ω(allReleased).Should(ContainElement(resource2))
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

					Context("and the resources emit logs", func() {
						It("emits them to the sink", func() {
							Eventually(tracker.InitCallCount).ShouldNot(Equal(0))

							_, logs, _ := tracker.InitArgsForCall(0)
							Ω(logs).ShouldNot(BeNil())

							logs.Write([]byte("hello from the resource"))

							Eventually(logBuffer).Should(gbytes.Say("hello from the resource"))

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

			Context("when the build is aborted", func() {
				BeforeEach(func() {
					resource1.InStub = func(input builds.Input) (io.Reader, builds.Input, builds.Config, error) {
						// return abort error to simulate fetching being aborted;
						// assert that the channel closed below
						return nil, builds.Input{}, builds.Config{}, ErrAborted
					}
				})

				It("aborts all resource activity", func() {
					Eventually(errored).Should(Receive(Equal(ErrAborted)))

					close(abort)

					_, _, resourceAbort := tracker.InitArgsForCall(0)
					Ω(resourceAbort).Should(BeClosed())
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

			Describe("after the build succeeds", func() {
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

		Context("when fetching the source fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				resource1.InReturns(nil, builds.Input{}, builds.Config{}, disaster)
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
					Name:            "first-resource",
					Type:            "raw",
					DestinationPath: "some/source/path",
				},
				{
					Name:            "second-resource",
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
					Name:            "first-resource",
					Type:            "raw",
					DestinationPath: "some/source/path",
					Version:         builds.Version{"key": "in-version-1"},
				},
				{
					Name:            "second-resource",
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
				Name:    "first-resource",
				Type:    "raw",
				Version: builds.Version{"key": "in-version-1"},
			}))

			Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
				Name:    "second-resource",
				Type:    "raw",
				Version: builds.Version{"key": "in-version-2"},
			}))
		})

		Context("and outputs are configured on the build", func() {
			var resource1 *resourcefakes.FakeResource
			var resource2 *resourcefakes.FakeResource

			BeforeEach(func() {
				succeededBuild.Build.Outputs = []builds.Output{
					{
						Name:   "first-resource",
						Type:   "git",
						Params: builds.Params{"key": "param-1"},
						Source: builds.Source{"uri": "http://first-uri"},
					},
					{
						Name:   "extra-output",
						Type:   "git",
						Params: builds.Params{"key": "param-2"},
						Source: builds.Source{"uri": "http://extra-uri"},
					},
				}

				resource1 = new(resourcefakes.FakeResource)
				resource2 = new(resourcefakes.FakeResource)

				resources := make(chan resource.Resource, 2)
				resources <- resource1
				resources <- resource2

				tracker.InitStub = func(typ string, logs io.Writer, abort <-chan struct{}) (resource.Resource, error) {
					return <-resources, nil
				}
			})

			Context("and streaming out succeeds", func() {
				BeforeEach(func() {
					wardenClient.Connection.WhenStreamingOut = func(string, string) (io.Reader, error) {
						return bytes.NewBufferString("streamed-out"), nil
					}
				})

				Context("when each output succeeds", func() {
					BeforeEach(func() {
						sync := make(chan bool)

						resource1.OutStub = func(src io.Reader, output builds.Output) (builds.Output, error) {
							<-sync
							output.Version = builds.Version{"key": "out-version-1"}
							output.Metadata = []builds.MetadataField{{Name: "name", Value: "out-meta-1"}}
							return output, nil
						}

						resource2.OutStub = func(src io.Reader, output builds.Output) (builds.Output, error) {
							close(sync)
							output.Version = builds.Version{"key": "out-version-3"}
							output.Metadata = []builds.MetadataField{{Name: "name", Value: "out-meta-3"}}
							return output, nil
						}
					})

					It("evaluates every output in parallel with the source and params", func() {
						Eventually(finished).Should(Receive())

						Ω(resource1.OutCallCount()).Should(Equal(1))

						streamIn, output := resource1.OutArgsForCall(0)
						Ω(output).Should(Equal(succeededBuild.Build.Outputs[0]))

						streamedIn, err := ioutil.ReadAll(streamIn)
						Ω(err).ShouldNot(HaveOccurred())

						Ω(string(streamedIn)).Should(Equal("streamed-out"))

						Ω(resource2.OutCallCount()).Should(Equal(1))

						streamIn, output = resource2.OutArgsForCall(0)
						Ω(output).Should(Equal(succeededBuild.Build.Outputs[1]))

						streamedIn, err = ioutil.ReadAll(streamIn)
						Ω(err).ShouldNot(HaveOccurred())

						Ω(string(streamedIn)).Should(Equal("streamed-out"))
					})

					It("reports the outputs", func() {
						var finishedBuild builds.Build
						Eventually(finished).Should(Receive(&finishedBuild))

						Ω(finishedBuild.Outputs).Should(HaveLen(3))

						Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
							Name:     "first-resource",
							Type:     "git",
							Source:   builds.Source{"uri": "http://first-uri"},
							Params:   builds.Params{"key": "param-1"},
							Version:  builds.Version{"key": "out-version-1"},
							Metadata: []builds.MetadataField{{Name: "name", Value: "out-meta-1"}},
						}))

						// Implicit output created for an input 'second-resource'
						Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
							Name:     "second-resource",
							Type:     "raw",
							Source:   nil,
							Params:   nil,
							Version:  builds.Version{"key": "in-version-2"},
							Metadata: nil,
						}))

						Ω(finishedBuild.Outputs).Should(ContainElement(builds.Output{
							Name:     "extra-output",
							Type:     "git",
							Source:   builds.Source{"uri": "http://extra-uri"},
							Params:   builds.Params{"key": "param-2"},
							Version:  builds.Version{"key": "out-version-3"},
							Metadata: []builds.MetadataField{{Name: "name", Value: "out-meta-3"}},
						}))
					})

					It("releases each resource", func() {
						Eventually(finished).Should(Receive())

						Ω(tracker.ReleaseCallCount()).Should(Equal(2))

						allReleased := []resource.Resource{
							tracker.ReleaseArgsForCall(0),
							tracker.ReleaseArgsForCall(1),
						}

						Ω(allReleased).Should(ContainElement(resource1))
						Ω(allReleased).Should(ContainElement(resource2))
					})
				})

				Context("when an output fails", func() {
					disaster := errors.New("oh no!")

					BeforeEach(func() {
						resource1.OutReturns(builds.Output{}, disaster)
					})

					It("sends the error result", func() {
						Eventually(errored).Should(Receive(Equal(disaster)))
					})

					It("releases each resource", func() {
						Eventually(errored).Should(Receive())

						Ω(tracker.ReleaseCallCount()).Should(Equal(2))

						allReleased := []resource.Resource{
							tracker.ReleaseArgsForCall(0),
							tracker.ReleaseArgsForCall(1),
						}

						Ω(allReleased).Should(ContainElement(resource1))
						Ω(allReleased).Should(ContainElement(resource2))
					})
				})

				Describe("logs emitted by output", func() {
					var logBuffer *gbytes.Buffer

					BeforeEach(func() {
						logBuffer = gbytes.NewBuffer()

						resource1.OutStub = func(src io.Reader, output builds.Output) (builds.Output, error) {
							defer GinkgoRecover()

							_, logs, _ := tracker.InitArgsForCall(0)

							Ω(logs).ShouldNot(BeNil())
							logs.Write([]byte("hello from outputter"))

							return output, nil
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

				Context("when the build is aborted", func() {
					BeforeEach(func() {
						resource1.OutStub = func(io.Reader, builds.Output) (builds.Output, error) {
							// return abort error to simulate fetching being aborted;
							// assert that the channel closed below
							return builds.Output{}, ErrAborted
						}
					})

					It("aborts all resource activity", func() {
						Eventually(errored).Should(Receive(Equal(ErrAborted)))

						close(abort)

						_, _, resourceAbort := tracker.InitArgsForCall(0)
						Ω(resourceAbort).Should(BeClosed())
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
})
