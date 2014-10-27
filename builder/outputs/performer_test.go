package outputs_test

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"sync"

	garden "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/garden/client/fake_api_client"
	"github.com/concourse/turbine"
	. "github.com/concourse/turbine/builder/outputs"
	"github.com/concourse/turbine/event"
	efakes "github.com/concourse/turbine/event/fakes"
	"github.com/concourse/turbine/event/testlog"
	"github.com/concourse/turbine/resource"
	rfakes "github.com/concourse/turbine/resource/fakes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Performer", func() {
	var (
		gardenClient *fake_api_client.FakeClient

		tracker   *rfakes.FakeTracker
		performer Performer

		container        garden.Container
		outputsToPerform []turbine.Output
		emitter          *efakes.FakeEmitter
		events           *testlog.EventLog
		abort            chan struct{}

		resource1 *rfakes.FakeResource
		resource2 *rfakes.FakeResource

		performedOutputs []turbine.Output
		performErr       error
	)

	BeforeEach(func() {
		var err error

		gardenClient = fake_api_client.New()

		gardenClient.Connection.CreateReturns("the-performing-container", nil)

		container, err = gardenClient.Create(garden.ContainerSpec{})
		Ω(err).ShouldNot(HaveOccurred())

		resource1 = new(rfakes.FakeResource)
		resource2 = new(rfakes.FakeResource)

		resources := make(chan resource.Resource, 2)
		resources <- resource1
		resources <- resource2

		tracker = new(rfakes.FakeTracker)
		tracker.InitStub = func(typ string, logs io.Writer, abort <-chan struct{}) (resource.Resource, error) {
			select {
			case r := <-resources:
				return r, nil
			default:
				Fail("initialized too many resources")
				panic("unreachable")
			}
		}

		outputsToPerform = []turbine.Output{
			turbine.Output{
				Name:   "banana",
				Type:   "some-type",
				Params: turbine.Params{"key": "banana-param"},
				Source: turbine.Source{"uri": "http://banana-uri"},
			},
			turbine.Output{
				Name:   "monkey",
				Type:   "some-type",
				Params: turbine.Params{"key": "monkey-param"},
				Source: turbine.Source{"uri": "http://monkey-uri"},
			},
		}

		emitter = new(efakes.FakeEmitter)

		events = &testlog.EventLog{}
		emitter.EmitEventStub = events.Add

		performer = NewParallelPerformer(tracker)
	})

	JustBeforeEach(func() {
		abort = make(chan struct{})
		performedOutputs, performErr = performer.PerformOutputs(container, outputsToPerform, emitter, abort)
	})

	performedOutput := func(output turbine.Output) turbine.Output {
		output.Version = turbine.Version{"version": output.Name + "-performed"}
		output.Metadata = []turbine.MetadataField{{Name: "output", Value: output.Name}}
		return output
	}

	Context("when streaming out succeeds", func() {
		BeforeEach(func() {
			gardenClient.Connection.StreamOutStub = func(handle string, srcPath string) (io.ReadCloser, error) {
				return ioutil.NopCloser(bytes.NewBufferString("streamed-out")), nil
			}
		})

		Context("when each output succeeds", func() {
			BeforeEach(func() {
				sync := new(sync.WaitGroup)
				sync.Add(2)

				resource1.OutStub = func(src io.Reader, output turbine.Output) (turbine.Output, error) {
					sync.Done()
					sync.Wait()
					return performedOutput(output), nil
				}

				resource2.OutStub = func(src io.Reader, output turbine.Output) (turbine.Output, error) {
					sync.Done()
					sync.Wait()
					return performedOutput(output), nil
				}
			})

			It("performs each output in parallel", func() {
				Ω(resource1.OutCallCount()).Should(Equal(1))
				streamIn, output1 := resource1.OutArgsForCall(0)
				streamedIn, err := ioutil.ReadAll(streamIn)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(string(streamedIn)).Should(Equal("streamed-out"))

				Ω(resource2.OutCallCount()).Should(Equal(1))
				streamIn, output2 := resource2.OutArgsForCall(0)
				streamedIn, err = ioutil.ReadAll(streamIn)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(string(streamedIn)).Should(Equal("streamed-out"))

				Ω([]turbine.Output{output1, output2}).Should(ConsistOf(outputsToPerform))
			})

			It("returns the outputs and emits events for each explicit output", func() {
				Ω(performedOutputs).Should(HaveLen(2))

				monkeyResult := turbine.Output{
					Name:     "monkey",
					Type:     "some-type",
					Source:   turbine.Source{"uri": "http://monkey-uri"},
					Params:   turbine.Params{"key": "monkey-param"},
					Version:  turbine.Version{"version": "monkey-performed"},
					Metadata: []turbine.MetadataField{{Name: "output", Value: "monkey"}},
				}

				bananaResult := turbine.Output{
					Name:     "banana",
					Type:     "some-type",
					Source:   turbine.Source{"uri": "http://banana-uri"},
					Params:   turbine.Params{"key": "banana-param"},
					Version:  turbine.Version{"version": "banana-performed"},
					Metadata: []turbine.MetadataField{{Name: "output", Value: "banana"}},
				}

				Ω(performedOutputs).Should(ContainElement(monkeyResult))
				Ω(events.Sent()).Should(ContainElement(event.Output{monkeyResult}))

				Ω(performedOutputs).Should(ContainElement(bananaResult))
				Ω(events.Sent()).Should(ContainElement(event.Output{bananaResult}))
			})

			It("releases each resource", func() {
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
				resource1.OutReturns(turbine.Output{Name: "failed-output"}, disaster)
			})

			It("returns an error", func() {
				Ω(performErr).Should(Equal(disaster))
			})

			It("emits an error event", func() {
				_, output := resource1.OutArgsForCall(0)

				Eventually(events.Sent).Should(ContainElement(event.Error{
					Message: "oh no!",
					Origin: event.Origin{
						Type: event.OriginTypeOutput,
						Name: output.Name,
					},
				}))
			})

			It("does not emit a bogus output event", func() {
				Ω(events.Sent()).ShouldNot(ContainElement(event.Output{
					turbine.Output{Name: "failed-output"},
				}))
			})

			It("releases each resource", func() {
				Ω(tracker.ReleaseCallCount()).Should(Equal(2))

				allReleased := []resource.Resource{
					tracker.ReleaseArgsForCall(0),
					tracker.ReleaseArgsForCall(1),
				}

				Ω(allReleased).Should(ContainElement(resource1))
				Ω(allReleased).Should(ContainElement(resource2))
			})
		})

		Describe("when the outputs emit logs", func() {
			BeforeEach(func() {
				for i, resource := range []*rfakes.FakeResource{resource1, resource2} {
					idx := i

					resource.OutStub = func(src io.Reader, output turbine.Output) (turbine.Output, error) {
						defer GinkgoRecover()

						_, logs, _ := tracker.InitArgsForCall(idx)

						Ω(logs).ShouldNot(BeNil())
						logs.Write([]byte("hello from outputter"))

						return output, nil
					}
				}
			})

			It("emits output events", func() {
				Eventually(events.Sent).Should(ContainElement(event.Log{
					Payload: "hello from outputter",
					Origin: event.Origin{
						Type: event.OriginTypeOutput,
						Name: "monkey",
					},
				}))

				Eventually(events.Sent).Should(ContainElement(event.Log{
					Payload: "hello from outputter",
					Origin: event.Origin{
						Type: event.OriginTypeOutput,
						Name: "banana",
					},
				}))
			})
		})

		Context("when the build is aborted", func() {
			errAborted := errors.New("aborted!")

			BeforeEach(func() {
				resource1.OutStub = func(io.Reader, turbine.Output) (turbine.Output, error) {
					// return abort error to simulate fetching being aborted;
					// assert that the channel closed below
					return turbine.Output{}, errAborted
				}
			})

			It("aborts all resource activity", func() {
				Ω(performErr).Should(Equal(errAborted))

				close(abort)

				_, _, resourceAbort := tracker.InitArgsForCall(0)
				Ω(resourceAbort).Should(BeClosed())
			})
		})
	})

	Context("when initializing the resource fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			tracker.InitReturns(nil, disaster)
		})

		It("returns the error", func() {
			Ω(performErr).Should(Equal(disaster))
		})

		It("emits an error event", func() {
			Eventually(events.Sent).Should(ContainElement(event.Error{
				Message: "oh no!",
				Origin: event.Origin{
					Type: event.OriginTypeOutput,
					Name: "monkey",
				},
			}))

			Eventually(events.Sent).Should(ContainElement(event.Error{
				Message: "oh no!",
				Origin: event.Origin{
					Type: event.OriginTypeOutput,
					Name: "banana",
				},
			}))
		})
	})

	Context("when streaming out fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			gardenClient.Connection.StreamOutReturns(nil, disaster)
		})

		It("returns the error", func() {
			Ω(performErr).Should(Equal(disaster))
		})

		It("emits an error event", func() {
			Eventually(events.Sent).Should(ContainElement(event.Error{
				Message: "oh no!",
				Origin: event.Origin{
					Type: event.OriginTypeOutput,
					Name: "monkey",
				},
			}))

			Eventually(events.Sent).Should(ContainElement(event.Error{
				Message: "oh no!",
				Origin: event.Origin{
					Type: event.OriginTypeOutput,
					Name: "banana",
				},
			}))
		})
	})
})
