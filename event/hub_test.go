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

	subscribe := func(from uint, events chan<- Event, versions chan<- Version) {
		subscribers.Add(1)
		go func() {
			defer subscribers.Done()
			hub.Subscribe(from, events, versions, stopSubscribers)
		}()
	}

	Context("when a version and event are emitted", func() {
		JustBeforeEach(func() {
			hub.EmitVersion("1.0")
			hub.EmitEvent(Start{Time: 123})
		})

		Describe("a late subscriber", func() {
			It("receives the event", func() {
				events := make(chan Event)
				versions := make(chan Version)

				subscribe(0, events, versions)

				Eventually(versions).Should(Receive(Equal(Version("1.0"))))
				Eventually(events).Should(Receive(Equal(Start{Time: 123})))
			})
		})

		Context("with a subscriber already listening", func() {
			var (
				receivedEvents   <-chan Event
				receivedVersions <-chan Version
			)

			BeforeEach(func() {
				events := make(chan Event)
				versions := make(chan Version)

				subscribe(0, events, versions)

				receivedEvents = events
				receivedVersions = versions
			})

			It("emits the event to the active subscriber", func() {
				Eventually(receivedVersions).Should(Receive(Equal(Version("1.0"))))
				Eventually(receivedEvents).Should(Receive(Equal(Start{Time: 123})))
			})
		})
	})

	Describe("subscribing", func() {
		Context("starting from a previous index", func() {
			BeforeEach(func() {
				hub.EmitVersion("1.0")

				for i := 0; i < 10; i++ {
					hub.EmitEvent(Start{Time: int64(i)})
				}
			})

			It("replays the event history", func() {
				events := make(chan Event)
				versions := make(chan Version)

				subscribe(6, events, versions)

				Consistently(versions).ShouldNot(Receive())
				Ω(<-events).Should(Equal(Start{Time: 5}))
				Ω(<-events).Should(Equal(Start{Time: 6}))
				Ω(<-events).Should(Equal(Start{Time: 7}))
				Ω(<-events).Should(Equal(Start{Time: 8}))
				Ω(<-events).Should(Equal(Start{Time: 9}))
			})

			Context("when another event is emitted", func() {
				It("consumes the event after the existing ones", func() {
					events := make(chan Event)
					versions := make(chan Version)

					subscribe(6, events, versions)

					Consistently(versions).ShouldNot(Receive())
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
				versions := make(chan Version)

				subscribe(1, events, versions)

				Eventually(events).Should(BeClosed())
				Eventually(versions).Should(BeClosed())
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
				versions := make(chan Version)

				subscribe(2, events, versions)

				Ω(<-events).Should(Equal(Start{Time: 3}))

				_, ok := <-events
				Ω(ok).Should(BeFalse())
			})

			Context("and someone subscribed", func() {
				var (
					receivedEvents   <-chan Event
					receivedVersions <-chan Version
				)

				BeforeEach(func() {
					events := make(chan Event)
					versions := make(chan Version)

					subscribe(0, events, versions)

					receivedEvents = events
					receivedVersions = versions
				})

				It("dispatches the events before closing the subscribers", func() {
					Ω(<-receivedEvents).Should(Equal(Start{Time: 1}))
					Ω(<-receivedEvents).Should(Equal(Start{Time: 2}))
					Ω(<-receivedEvents).Should(Equal(Start{Time: 3}))
					Eventually(receivedEvents).Should(BeClosed())
					Eventually(receivedVersions).Should(BeClosed())
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
})
