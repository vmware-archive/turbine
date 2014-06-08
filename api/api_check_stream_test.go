package api_test

import (
	"encoding/json"
	"errors"
	"io"

	"code.google.com/p/go.net/websocket"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/resource/fakes"
)

var _ = Describe("GET /checks/stream", func() {
	var input builds.Input

	var conn io.ReadWriteCloser

	BeforeEach(func() {
		input = builds.Input{
			Type:    "git",
			Source:  builds.Source{"uri": "example.com"},
			Version: builds.Version{"ref": "foo"},
		}
	})

	BeforeEach(func() {
		var err error

		conn, err = websocket.Dial("ws://"+server.Listener.Addr().String()+"/checks/stream", "", "http://0.0.0.0")
		Ω(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		conn.Close()
	})

	Context("when an input is sent", func() {
		JustBeforeEach(func() {
			err := json.NewEncoder(conn).Encode(&input)
			Ω(err).ShouldNot(HaveOccurred())
		})

		Context("and initializing the resource succeeds", func() {
			var resource *fakes.FakeResource

			BeforeEach(func() {
				resource = new(fakes.FakeResource)

				tracker.InitReturns(resource, nil)
			})

			Context("and the resource yields versions", func() {
				versions := []builds.Version{{"version": "1"}, {"version": "2"}}

				BeforeEach(func() {
					resource.CheckReturns(versions, nil)
				})

				It("writes them to the connection", func() {
					Eventually(resource.CheckCallCount).Should(Equal(1))
					Ω(resource.CheckArgsForCall(0)).Should(Equal(input))

					var returnedVersions []builds.Version
					err := json.NewDecoder(conn).Decode(&returnedVersions)
					Ω(err).ShouldNot(HaveOccurred())

					Ω(returnedVersions).Should(Equal(versions))
				})
			})

			Context("when the check fails", func() {
				BeforeEach(func() {
					resource.CheckReturns(nil, errors.New("oh no!"))
				})

				It("closes the connection", func() {
					Eventually(func() error {
						_, err := conn.Read([]byte{})
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
					_, err := conn.Read([]byte{})
					return err
				}).Should(HaveOccurred())
			})
		})
	})

	Describe("draining", func() {
		It("closes the connection", func() {
			close(drain)

			Eventually(func() error {
				_, err := conn.Write([]byte{})
				return err
			}).Should(HaveOccurred())
		})
	})

	Context("when an invalid payload is sent", func() {
		JustBeforeEach(func() {
			_, err := conn.Write([]byte("ß"))
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("closes the connection", func() {
			Eventually(func() error {
				_, err := conn.Read([]byte{})
				return err
			}).Should(HaveOccurred())
		})
	})
})
