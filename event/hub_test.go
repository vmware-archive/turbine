package event_test

import (
	"sync"

	. "github.com/concourse/turbine/event"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Hub", func() {
	var (
		hub *Hub

		stopSubscribers chan struct{}
		subscribers     *sync.WaitGroup
	)

	BeforeEach(func() {
		hub = NewHub()

		stopSubscribers = make(chan struct{})
		subscribers = new(sync.WaitGroup)
	})

	AfterEach(func() {
		close(stopSubscribers)
		subscribers.Wait()
	})

	subscribe := func(from uint, events chan<- Event) {
		subscribers.Add(1)
		go func() {
			defer subscribers.Done()
			hub.Subscribe(from, events, stopSubscribers)
		}()
	}

	Context("when an event is emitted", func() {
		JustBeforeEach(func() {
			hub.EmitEvent(Start{Time: 123})
		})

		Describe("a late subscriber", func() {
			It("receives the event", func() {
				events := make(chan Event)

				subscribe(0, events)

				Eventually(events).Should(Receive(Equal(Start{Time: 123})))
			})
		})

		Context("with a subscriber already listening", func() {
			var (
				receivedEvents <-chan Event
			)

			BeforeEach(func() {
				events := make(chan Event)

				subscribe(0, events)

				receivedEvents = events
			})

			It("emits the event to the active subscriber", func() {
				Eventually(receivedEvents).Should(Receive(Equal(Start{Time: 123})))
			})
		})
	})

	Describe("subscribing", func() {
		Context("starting from a previous index", func() {
			BeforeEach(func() {
				for i := 0; i < 10; i++ {
					hub.EmitEvent(Start{Time: int64(i)})
				}
			})

			It("replays the event history", func() {
				events := make(chan Event)

				subscribe(5, events)

				Ω(<-events).Should(Equal(Start{Time: 5}))
				Ω(<-events).Should(Equal(Start{Time: 6}))
				Ω(<-events).Should(Equal(Start{Time: 7}))
				Ω(<-events).Should(Equal(Start{Time: 8}))
				Ω(<-events).Should(Equal(Start{Time: 9}))
			})

			Context("when another event is emitted", func() {
				It("consumes the event after the existing ones", func() {
					events := make(chan Event)

					subscribe(5, events)

					Ω(<-events).Should(Equal(Start{Time: 5}))
					Ω(<-events).Should(Equal(Start{Time: 6}))
					Ω(<-events).Should(Equal(Start{Time: 7}))
					Ω(<-events).Should(Equal(Start{Time: 8}))
					Ω(<-events).Should(Equal(Start{Time: 9}))
					Consistently(events).ShouldNot(Receive())

					hub.EmitEvent(Start{Time: 10})
					Ω(<-events).Should(Equal(Start{Time: 10}))
				})
			})
		})

		Context("when the given index is out of bounds", func() {
			It("closes the event and version destinations", func() {
				events := make(chan Event)

				subscribe(1, events)

				Eventually(events).Should(BeClosed())
			})
		})
	})

	Describe("closing", func() {
		JustBeforeEach(func() {
			hub.Close()
		})

		Context("after events are emitted", func() {
			BeforeEach(func() {
				hub.EmitEvent(Start{Time: 1})
				hub.EmitEvent(Start{Time: 2})
				hub.EmitEvent(Start{Time: 3})
			})

			It("dispatches the previous events before closing the subscription", func() {
				events := make(chan Event)

				subscribe(2, events)

				Ω(<-events).Should(Equal(Start{Time: 3}))

				_, ok := <-events
				Ω(ok).Should(BeFalse())
			})

			Context("and someone subscribed", func() {
				var (
					receivedEvents <-chan Event
				)

				BeforeEach(func() {
					events := make(chan Event)

					subscribe(0, events)

					receivedEvents = events
				})

				It("dispatches the events before closing the subscribers", func() {
					Ω(<-receivedEvents).Should(Equal(Start{Time: 1}))
					Ω(<-receivedEvents).Should(Equal(Start{Time: 2}))
					Ω(<-receivedEvents).Should(Equal(Start{Time: 3}))
					Eventually(receivedEvents).Should(BeClosed())
				})
			})
		})

		Describe("twice", func() {
			It("does not blow up", func() {
				hub.Close()
			})
		})

		Describe("and then emitting events", func() {
			It("does not blow up", func() {
				hub.EmitEvent(Start{Time: 123})
			})
		})
	})

	Describe("getting all events", func() {
		It("returns all events emitted to the hub", func() {
			hub.EmitEvent(Version("1.0"))
			hub.EmitEvent(Start{Time: 1})
			hub.EmitEvent(Start{Time: 2})
			hub.EmitEvent(Start{Time: 3})

			Ω(hub.Events()).Should(Equal([]Event{
				Version("1.0"),
				Start{Time: 1},
				Start{Time: 2},
				Start{Time: 3},
			}))
		})
	})
})
