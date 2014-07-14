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
	"sync"

	"github.com/cloudfoundry-incubator/garden/warden"
	wfakes "github.com/cloudfoundry-incubator/garden/warden/fakes"
	"github.com/concourse/turbine/api/hijack"
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

		payload, err = json.Marshal(warden.ProcessSpec{
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
		var process *wfakes.FakeProcess
		var wait *sync.WaitGroup

		BeforeEach(func() {
			process = new(wfakes.FakeProcess)
			wait = new(sync.WaitGroup)

			wait.Add(1)
			process.WaitStub = func() (int, error) {
				wait.Wait()
				return 0, nil
			}

			scheduler.HijackReturns(process, nil)
		})

		It("hijacks the build via the scheduler", func() {
			guid, spec, _ := scheduler.HijackArgsForCall(0)
			Ω(guid).Should(Equal("some-build-guid"))
			Ω(spec).Should(Equal(warden.ProcessSpec{
				Path: "bash",
				Args: []string{"-l"},
			}))
		})

		It("waits on the process", func() {
			wait.Done()
			Eventually(process.WaitCallCount).Should(Equal(1))
		})

		Context("when the process prints stdout and stderr", func() {
			BeforeEach(func() {
				scheduler.HijackStub = func(guid string, spec warden.ProcessSpec, io warden.ProcessIO) (warden.Process, error) {
					Ω(io.Stdout).ShouldNot(BeZero())
					Ω(io.Stderr).ShouldNot(BeZero())

					_, err := fmt.Fprintf(io.Stdout, "hello client out\n")
					Ω(err).ShouldNot(HaveOccurred())

					_, err = fmt.Fprintf(io.Stderr, "hello client err\n")
					Ω(err).ShouldNot(HaveOccurred())

					return process, nil
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
			It("forwards to the process's stdin", func() {
				err := encoder.Encode(hijack.ProcessPayload{
					Stdin: []byte("some stdin\n"),
				})
				Ω(err).ShouldNot(HaveOccurred())

				_, _, io := scheduler.HijackArgsForCall(0)

				line, err := bufio.NewReader(io.Stdin).ReadBytes('\n')
				Ω(err).ShouldNot(HaveOccurred())
				Ω(string(line)).Should(Equal("some stdin\n"))
			})
		})

		Context("when a window size payload is received", func() {
			It("forwards window size paylods to the process", func() {
				err := encoder.Encode(hijack.ProcessPayload{
					WindowSize: &hijack.WindowSize{
						Columns: 80,
						Rows:    24,
					},
				})
				Ω(err).ShouldNot(HaveOccurred())

				Eventually(process.SetWindowSizeCallCount).Should(Equal(1))

				cols, rows := process.SetWindowSizeArgsForCall(0)
				Ω(cols).Should(Equal(80))
				Ω(rows).Should(Equal(24))
			})
		})

		Context("when the connection breaks", func() {
			It("closes the process's stdin", func() {
				conn.Close()

				_, _, io := scheduler.HijackArgsForCall(0)

				_, err := bufio.NewReader(io.Stdin).ReadBytes('\n')
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when the process exits", func() {
			It("closes the connection", func() {
				wait.Done()

				_, err := br.ReadBytes('\n')
				Ω(err).Should(HaveOccurred())
			})
		})
	})
})
