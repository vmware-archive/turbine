package api_test

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"

	garden "github.com/cloudfoundry-incubator/garden/api"
	gfakes "github.com/cloudfoundry-incubator/garden/api/fakes"
	"github.com/concourse/turbine"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("POST /builds/:guid/hijack", func() {
	var payload []byte

	var response *http.Response
	var conn net.Conn
	var encoder *gob.Encoder
	var br *bufio.Reader

	BeforeEach(func() {
		var err error

		payload, err = json.Marshal(garden.ProcessSpec{
			Path: "bash",
			Args: []string{"-l"},
		})
		Ω(err).ShouldNot(HaveOccurred())
	})

	JustBeforeEach(func() {
		var err error

		conn, err = net.Dial("tcp", server.Listener.Addr().String())
		Ω(err).ShouldNot(HaveOccurred())

		req, err := http.NewRequest("POST", server.URL+"/builds/some-build-guid/hijack", bytes.NewBuffer(payload))
		Ω(err).ShouldNot(HaveOccurred())

		client := httputil.NewClientConn(conn, nil)

		response, err = client.Do(req)
		Ω(err).ShouldNot(HaveOccurred())

		conn, br = client.Hijack()

		encoder = gob.NewEncoder(conn)
	})

	AfterEach(func() {
		conn.Close()
	})

	Context("when hijacking succeeds", func() {
		var process *gfakes.FakeProcess

		BeforeEach(func() {
			process = new(gfakes.FakeProcess)

			scheduler.HijackReturns(process, nil)
		})

		It("hijacks the build via the scheduler", func() {
			Eventually(scheduler.HijackCallCount).Should(Equal(1))

			guid, spec, _ := scheduler.HijackArgsForCall(0)
			Ω(guid).Should(Equal("some-build-guid"))
			Ω(spec).Should(Equal(garden.ProcessSpec{
				Path: "bash",
				Args: []string{"-l"},
			}))
		})

		It("waits on the process", func() {
			Eventually(process.WaitCallCount).Should(Equal(1))
		})

		Context("when the process prints stdout and stderr", func() {
			BeforeEach(func() {
				scheduler.HijackStub = func(guid string, spec garden.ProcessSpec, io garden.ProcessIO) (garden.Process, error) {
					Ω(io.Stdout).ShouldNot(BeZero())
					Ω(io.Stderr).ShouldNot(BeZero())

					_, err := fmt.Fprintf(io.Stdout, "hello client out\n")
					Ω(err).ShouldNot(HaveOccurred())

					_, err = fmt.Fprintf(io.Stderr, "hello client err\n")
					Ω(err).ShouldNot(HaveOccurred())

					return new(gfakes.FakeProcess), nil
				}
			})

			It("streams stdout and stderr to the response", func() {
				line, err := br.ReadBytes('\n')
				Ω(err).ShouldNot(HaveOccurred())
				Ω(string(line)).Should(Equal("hello client out\n"))

				line, err = br.ReadBytes('\n')
				Ω(err).ShouldNot(HaveOccurred())
				Ω(string(line)).Should(Equal("hello client err\n"))
			})
		})

		Context("when a stdin payload is received", func() {
			BeforeEach(func() {
				process.WaitStub = func() (int, error) {
					select {}
				}
			})

			It("forwards to the process's stdin", func() {
				err := encoder.Encode(turbine.HijackPayload{
					Stdin: []byte("some stdin\n"),
				})
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(scheduler.HijackCallCount).Should(Equal(1))

				_, _, io := scheduler.HijackArgsForCall(0)

				line, err := bufio.NewReader(io.Stdin).ReadBytes('\n')
				Ω(err).ShouldNot(HaveOccurred())
				Ω(string(line)).Should(Equal("some stdin\n"))
			})
		})

		Context("when a window size payload is received", func() {
			BeforeEach(func() {
				process.WaitStub = func() (int, error) {
					select {}
				}
			})

			It("forwards tty spec paylods to the process", func() {
				ttySpec := &garden.TTYSpec{
					WindowSize: &garden.WindowSize{
						Columns: 80,
						Rows:    24,
					},
				}

				err := encoder.Encode(turbine.HijackPayload{
					TTYSpec: ttySpec,
				})
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(process.SetTTYCallCount).Should(Equal(1))

				Ω(process.SetTTYArgsForCall(0)).Should(Equal(*ttySpec))
			})
		})

		Context("when the connection breaks", func() {
			It("closes the process's stdin", func() {
				conn.Close()

				Eventually(scheduler.HijackCallCount).Should(Equal(1))

				_, _, io := scheduler.HijackArgsForCall(0)

				_, err := bufio.NewReader(io.Stdin).ReadBytes('\n')
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when the process exits", func() {
			It("closes the connection", func() {
				_, err := br.ReadBytes('\n')
				Ω(err).Should(HaveOccurred())
			})
		})
	})
})
