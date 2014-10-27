package api_test

import (
	"errors"

	"github.com/gorilla/websocket"

	"github.com/concourse/turbine"
	"github.com/concourse/turbine/resource/fakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("GET /checks/stream", func() {
	var input turbine.Input

	var conn *websocket.Conn

	BeforeEach(func() {
		input = turbine.Input{
			Type:    "git",
			Source:  turbine.Source{"uri": "example.com"},
			Version: turbine.Version{"ref": "foo"},
		}
	})

	BeforeEach(func() {
		var err error

		dialer := &websocket.Dialer{}

		conn, _, err = dialer.Dial("ws://"+server.Listener.Addr().String()+"/checks/stream", nil)
		Ω(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		conn.Close()
	})

	Context("when an input is sent", func() {
		JustBeforeEach(func() {
			err := conn.WriteJSON(input)
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("and initializing the resource succeeds", func() {
			var resource *fakes.FakeResource

			BeforeEach(func() {
				resource = new(fakes.FakeResource)

				tracker.InitReturns(resource, nil)
			})

			Context("and the resource yields versions", func() {
				versions := []turbine.Version{{"version": "1"}, {"version": "2"}}

				BeforeEach(func() {
					resource.CheckReturns(versions, nil)
				})

				It("writes them to the connection", func() {
					Eventually(resource.CheckCallCount).Should(Equal(1))
					Ω(resource.CheckArgsForCall(0)).Should(Equal(input))

					var returnedVersions []turbine.Version
					err := conn.ReadJSON(&returnedVersions)
					Ω(err).ShouldNot(HaveOccurred())

					Ω(returnedVersions).Should(Equal(versions))
				})
			})

			Describe("draining", func() {
				It("aborts the check", func() {
					Eventually(tracker.InitCallCount).Should(Equal(1))

					_, _, abort := tracker.InitArgsForCall(0)

					Ω(abort).ShouldNot(BeClosed())

					close(drain)

					Ω(abort).Should(BeClosed())
				})
			})

			Context("when the check fails", func() {
				BeforeEach(func() {
					resource.CheckReturns(nil, errors.New("oh no!"))
				})

				It("closes the connection", func() {
					Eventually(func() error {
						_, _, err := conn.ReadMessage()
						return err
					}).Should(HaveOccurred())
				})
			})

			Context("and the connection closes", func() {
				It("releases the resource", func() {
					err := conn.Close()
					Ω(err).ShouldNot(HaveOccurred())

					Eventually(tracker.ReleaseCallCount).Should(Equal(1))
					Ω(tracker.ReleaseArgsForCall(0)).Should(Equal(resource))
				})
			})
		})

		Context("when initializing the resource fails", func() {
			BeforeEach(func() {
				tracker.InitReturns(nil, errors.New("oh no!"))
			})

			It("closes the connection", func() {
				Eventually(func() error {
					_, _, err := conn.ReadMessage()
					return err
				}).Should(HaveOccurred())
			})
		})
	})

	Describe("draining", func() {
		It("closes the connection", func() {
			close(drain)

			Eventually(func() error {
				return conn.WriteMessage(websocket.BinaryMessage, nil)
			}).Should(HaveOccurred())
		})
	})

	Context("when an invalid payload is sent", func() {
		JustBeforeEach(func() {
			err := conn.WriteMessage(websocket.BinaryMessage, []byte("ß"))
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("closes the connection", func() {
			Eventually(func() error {
				_, _, err := conn.ReadMessage()
				return err
			}).Should(HaveOccurred())
		})
	})
})
