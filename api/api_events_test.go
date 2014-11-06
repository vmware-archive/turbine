package api_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/concourse/turbine/event"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vito/go-sse/sse"
)

var _ = Describe("GET /builds/:guid/events", func() {
	var (
		events chan event.Event
		stop   chan struct{}

		request  *http.Request
		response *http.Response
	)

	BeforeEach(func() {
		var err error

		events = make(chan event.Event, 10)
		stop = make(chan struct{})

		scheduler.SubscribeReturns(events, stop, nil)

		request, err = http.NewRequest("GET", server.URL+"/builds/some-build-guid/events", nil)
		Ω(err).ShouldNot(HaveOccurred())
	})

	JustBeforeEach(func() {
		var err error

		response, err = client.Do(request)
		Ω(err).ShouldNot(HaveOccurred())
	})

	It("returns 200", func() {
		Ω(response.StatusCode).Should(Equal(http.StatusOK))
	})

	It("returns Content-Type as text/event-stream", func() {
		Ω(response.Header.Get("Content-Type")).Should(Equal("text/event-stream; charset=utf-8"))
		Ω(response.Header.Get("Cache-Control")).Should(Equal("no-cache, no-store, must-revalidate"))
		Ω(response.Header.Get("Connection")).Should(Equal("keep-alive"))
	})

	It("subscribes to the build via the scheduler", func() {
		Ω(scheduler.SubscribeCallCount()).Should(Equal(1))

		guid, from := scheduler.SubscribeArgsForCall(0)
		Ω(guid).Should(Equal("some-build-guid"))
		Ω(from).Should(Equal(uint(0)))
	})

	Context("when the Last-Event-ID header is given", func() {
		BeforeEach(func() {
			request.Header.Set("Last-Event-ID", "42")
		})

		It("subscribes from after the given id", func() {
			Ω(scheduler.SubscribeCallCount()).Should(Equal(1))

			guid, from := scheduler.SubscribeArgsForCall(0)
			Ω(guid).Should(Equal("some-build-guid"))
			Ω(from).Should(Equal(uint(42 + 1)))
		})

		Context("but it's set to something bogus", func() {
			BeforeEach(func() {
				request.Header.Set("Last-Event-ID", "bogus")
			})

			It("returns 400", func() {
				Ω(response.StatusCode).Should(Equal(http.StatusBadRequest))
			})
		})
	})

	Describe("event emitting", func() {
		It("emits events from the subscription as server-sent events", func() {
			reader := sse.NewReader(response.Body)

			events <- event.Start{Time: 1}

			ev, err := reader.Next()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(ev.ID).Should(Equal("0"))
			Ω(ev.Name).Should(Equal(string(event.EventTypeStart)))

			var message event.Start
			err = json.Unmarshal(ev.Data, &message)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(message).Should(Equal(event.Start{Time: 1}))

			events <- event.Start{Time: 2}

			ev, err = reader.Next()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(ev.ID).Should(Equal("1"))
			Ω(ev.Name).Should(Equal(string(event.EventTypeStart)))

			err = json.Unmarshal(ev.Data, &message)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(message).Should(Equal(event.Start{Time: 2}))
		})

		Context("when the event stream finishes", func() {
			BeforeEach(func() {
				close(events)
			})

			It("closes the response", func() {
				reader := sse.NewReader(response.Body)

				_, err := reader.Next()
				Ω(err).Should(Equal(io.EOF))
			})
		})

		Describe("closing the connection", func() {
			It("stops the subscription", func() {
				err := response.Body.Close()
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(stop).Should(BeClosed())
			})
		})

		Context("when events already were emitted", func() {
			BeforeEach(func() {
				events <- event.Start{Time: 1}
				events <- event.Start{Time: 2}
				events <- event.Start{Time: 3}
				events <- event.Start{Time: 4}
				events <- event.Start{Time: 5}
			})

			It("emits them starting from the first one", func() {
				reader := sse.NewReader(response.Body)

				for i := 0; i < 5; i++ {
					ev, err := reader.Next()
					Ω(err).ShouldNot(HaveOccurred())

					Ω(ev.ID).Should(Equal(fmt.Sprintf("%d", i)))
					Ω(ev.Name).Should(Equal(string(event.EventTypeStart)))

					var message event.Start
					err = json.Unmarshal(ev.Data, &message)
					Ω(err).ShouldNot(HaveOccurred())

					Ω(message).Should(Equal(event.Start{Time: 1 + int64(i)}))
				}
			})

			Context("and the request specifies Last-Event-ID", func() {
				BeforeEach(func() {
					request.Header.Set("Last-Event-ID", "42")
				})

				It("emits the events with their ids offset", func() {
					reader := sse.NewReader(response.Body)

					for i := 0; i < 5; i++ {
						ev, err := reader.Next()
						Ω(err).ShouldNot(HaveOccurred())

						Ω(ev.ID).Should(Equal(fmt.Sprintf("%d", 42+1+i)))
						Ω(ev.Name).Should(Equal(string(event.EventTypeStart)))

						var message event.Start
						err = json.Unmarshal(ev.Data, &message)
						Ω(err).ShouldNot(HaveOccurred())

						Ω(message).Should(Equal(event.Start{Time: 1 + int64(i)}))
					}
				})
			})
		})
	})

	Context("when subscribing fails", func() {
		BeforeEach(func() {
			scheduler.SubscribeReturns(nil, nil, errors.New("oh no!"))
		})

		It("returns 500", func() {
			Ω(response.StatusCode).Should(Equal(http.StatusNotFound))
		})
	})
})
