package event_test

import (
	"net/http"
	"time"

	. "github.com/concourse/turbine/event"
	"github.com/gorilla/websocket"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

var _ = Describe("Emitting events", func() {
	var (
		consumer *ghttp.Server

		successfulHandler http.HandlerFunc
		requestedHeaders  <-chan http.Header
		consumedMessages  <-chan Message

		triggerDrain chan<- struct{}

		emitter Emitter

		event Event

		upgrader = websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool {
				return true
			},
		}
	)

	BeforeEach(func() {
		consumer = ghttp.NewServer()

		headers := make(chan http.Header, 1)
		messages := make(chan Message)

		consumer.AppendHandlers()

		successfulHandler = func(w http.ResponseWriter, r *http.Request) {
			headers <- r.Header

			conn, err := upgrader.Upgrade(w, r, nil)
			Ω(err).ShouldNot(HaveOccurred())

			var version VersionMessage
			err = conn.ReadJSON(&version)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(version.Version).Should(Equal(VERSION))

			for {
				var msg Message
				err := conn.ReadJSON(&msg)
				if err != nil {
					break
				}

				messages <- msg
			}
		}

		requestedHeaders = headers
		consumedMessages = messages

		consumerAddr := consumer.HTTPTestServer.Listener.Addr().String()

		drain := make(chan struct{})
		triggerDrain = drain

		emitter = NewWebSocketEmitter("ws://username:password@"+consumerAddr, drain)

		event = Log{
			Payload: "sup",
			Origin: Origin{
				Type: OriginTypeInput,
				Name: "some-input",
			},
		}
	})

	JustBeforeEach(func() {
		emitter.EmitEvent(event)
	})

	Context("when the consumer is working", func() {
		BeforeEach(func() {
			consumer.AppendHandlers(successfulHandler)
		})

		It("sends a log message to the consumer", func() {
			Eventually(consumedMessages).Should(Receive(Equal(Message{
				Event: event,
			})))
		})

		It("requests with standard basic auth headers", func() {
			headers := <-requestedHeaders

			// username:password encoded
			Ω(headers.Get("Authorization")).Should(Equal("Basic dXNlcm5hbWU6cGFzc3dvcmQ="))
		})
	})

	Context("when the consumer is failing", func() {
		var consumerFailed <-chan struct{}

		BeforeEach(func() {
			failed := make(chan struct{})
			consumerFailed = failed

			consumer.AppendHandlers(
				func(w http.ResponseWriter, r *http.Request) {
					conn, err := upgrader.Upgrade(w, r, nil)
					Ω(err).ShouldNot(HaveOccurred())

					close(failed)

					conn.Close()
				},
				successfulHandler,
			)
		})

		It("retries", func() {
			Eventually(consumerFailed).Should(BeClosed())

			// retrying has a 1s delay
			Eventually(consumedMessages, 5*time.Second).Should(Receive(Equal(Message{
				Event: event,
			})))
		})

		Context("while draining", func() {
			BeforeEach(func() {
				close(triggerDrain)
			})

			It("gives up", func() {
				Eventually(consumerFailed).Should(BeClosed())
				Consistently(consumedMessages, 2*time.Second).ShouldNot(Receive())
			})
		})
	})
})
