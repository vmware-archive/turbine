package inputs_test

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/concourse/turbine/api/builds"
	. "github.com/concourse/turbine/builder/inputs"
	"github.com/concourse/turbine/event"
	efakes "github.com/concourse/turbine/event/fakes"
	"github.com/concourse/turbine/event/testlog"
	"github.com/concourse/turbine/resource"
	rfakes "github.com/concourse/turbine/resource/fakes"
)

var _ = Describe("Inputs", func() {
	var (
		tracker *rfakes.FakeTracker

		inputs  []builds.Input
		emitter *efakes.FakeEmitter
		events  *testlog.EventLog
		abort   chan struct{}

		fetcher Fetcher

		fetchedInputs []FetchedInput
		fetchErr      error
	)

	BeforeEach(func() {
		tracker = new(rfakes.FakeTracker)

		fetcher = NewParallelFetcher(tracker)

		inputs = []builds.Input{
			{
				Name: "first-resource",
				Type: "raw",
			},
			{
				Name: "second-resource",
				Type: "raw",
			},
		}

		emitter = new(efakes.FakeEmitter)

		events = &testlog.EventLog{}
		emitter.EmitEventStub = events.Add

		abort = make(chan struct{})
	})

	Describe("Fetch", func() {
		var resource1 *rfakes.FakeResource
		var resource2 *rfakes.FakeResource

		BeforeEach(func() {
			resource1 = new(rfakes.FakeResource)
			resource2 = new(rfakes.FakeResource)

			resources := make(chan resource.Resource, 2)
			resources <- resource1
			resources <- resource2

			tracker.InitStub = func(typ string, logs io.Writer, abort <-chan struct{}) (resource.Resource, error) {
				return <-resources, nil
			}
		})

		JustBeforeEach(func() {
			fetchedInputs, fetchErr = fetcher.Fetch(inputs, emitter, abort)
		})

		Context("when each resource in action succeeds", func() {
			BeforeEach(func() {
				resource1.InStub = func(input builds.Input) (io.Reader, builds.Input, builds.Config, error) {
					sourceStream := bytes.NewBufferString("some-data-1")
					input.Version = builds.Version{"version": "1"}
					input.Metadata = []builds.MetadataField{{Name: "key", Value: "meta-1"}}
					return sourceStream, input, builds.Config{}, nil
				}

				resource2.InStub = func(input builds.Input) (io.Reader, builds.Input, builds.Config, error) {
					sourceStream := bytes.NewBufferString("some-data-2")
					input.Version = builds.Version{"version": "2"}
					input.Metadata = []builds.MetadataField{{Name: "key", Value: "meta-2"}}
					return sourceStream, input, builds.Config{}, nil
				}
			})

			It("does not return an error", func() {
				Ω(fetchErr).ShouldNot(HaveOccurred())
			})

			It("returns the fetched inputs", func() {
				Ω(fetchedInputs[0].Input).Should(Equal(builds.Input{
					Name:     "first-resource",
					Type:     "raw",
					Version:  builds.Version{"version": "1"},
					Metadata: []builds.MetadataField{{Name: "key", Value: "meta-1"}},
				}))

				Ω(fetchedInputs[0].Stream).ShouldNot(BeNil())
				stream, err := ioutil.ReadAll(fetchedInputs[0].Stream)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(string(stream)).Should(Equal("some-data-1"))

				Ω(fetchedInputs[1].Input).Should(Equal(builds.Input{
					Name:     "second-resource",
					Type:     "raw",
					Version:  builds.Version{"version": "2"},
					Metadata: []builds.MetadataField{{Name: "key", Value: "meta-2"}},
				}))

				Ω(fetchedInputs[1].Stream).ShouldNot(BeNil())
				stream, err = ioutil.ReadAll(fetchedInputs[1].Stream)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(string(stream)).Should(Equal("some-data-2"))
			})

			It("emits input events", func() {
				Eventually(events.Sent).Should(ContainElement(event.Input{
					Input: builds.Input{
						Name:     "first-resource",
						Type:     "raw",
						Version:  builds.Version{"version": "1"},
						Metadata: []builds.MetadataField{{Name: "key", Value: "meta-1"}},
					},
				}))

				Eventually(events.Sent).Should(ContainElement(event.Input{
					Input: builds.Input{
						Name:     "second-resource",
						Type:     "raw",
						Version:  builds.Version{"version": "2"},
						Metadata: []builds.MetadataField{{Name: "key", Value: "meta-2"}},
					},
				}))
			})

			Describe("releasing the returned inputs", func() {
				It("releases the resource", func() {
					fetchedInputs[0].Release()
					Ω(tracker.ReleaseCallCount()).Should(Equal(1))
					Ω(tracker.ReleaseArgsForCall(0)).Should(Equal(resource1))

					fetchedInputs[1].Release()
					Ω(tracker.ReleaseCallCount()).Should(Equal(2))
					Ω(tracker.ReleaseArgsForCall(1)).Should(Equal(resource2))
				})
			})

			Context("when an input provides build configuration", func() {
				BeforeEach(func() {
					resource2.InStub = func(input builds.Input) (io.Reader, builds.Input, builds.Config, error) {
						sourceStream := bytes.NewBufferString("some-data-2")

						input.Version = builds.Version{"key": "version-2"}
						input.Metadata = []builds.MetadataField{{Name: "key", Value: "meta-2"}}

						config := builds.Config{
							Image: "build-config-image",

							Params: map[string]string{
								"FOO":         "build-config-foo",
								"CONFIG_ONLY": "build-config-only",
							},
						}

						return sourceStream, input, config, nil
					}
				})

				It("returns it on the fetched input", func() {
					Ω(fetchedInputs[1].Config).Should(Equal(builds.Config{
						Image: "build-config-image",

						Params: map[string]string{
							"FOO":         "build-config-foo",
							"CONFIG_ONLY": "build-config-only",
						},
					}))
				})
			})

			Context("when the inputs emit logs", func() {
				BeforeEach(func() {
					tracker.InitStub = func(typ string, logs io.Writer, abort <-chan struct{}) (resource.Resource, error) {
						go func() {
							defer GinkgoRecover()

							_, err := logs.Write([]byte("hello from the resource"))
							Ω(err).ShouldNot(HaveOccurred())
						}()

						return new(rfakes.FakeResource), nil
					}
				})

				It("emits a build log event", func() {
					Eventually(events.Sent).Should(ContainElement(event.Log{
						Payload: "hello from the resource",
						Origin: event.Origin{
							Type: event.OriginTypeInput,
							Name: "first-resource",
						},
					}))
				})
			})

			Context("when fetching is aborted", func() {
				BeforeEach(func() {
					close(abort)
				})

				It("aborts all resource activity", func() {
					_, _, resourceAbort := tracker.InitArgsForCall(0)
					Ω(resourceAbort).Should(BeClosed())
				})
			})
		})

		Context("when initializing an input resource fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				tracker.InitReturns(nil, disaster)
			})

			It("returns the error", func() {
				Ω(fetchErr).Should(Equal(disaster))
			})
		})

		Context("when fetching the source fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				resource1.InReturns(nil, builds.Input{}, builds.Config{}, disaster)
			})

			It("returns the error", func() {
				Ω(fetchErr).Should(Equal(disaster))
			})
		})
	})
})
