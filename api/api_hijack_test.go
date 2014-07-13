package api_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"

	"github.com/cloudfoundry-incubator/garden/warden"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("POST /builds/:guid/hijack", func() {
	var payload []byte

	var response *http.Response
	var conn net.Conn
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
	})

	AfterEach(func() {
		conn.Close()
	})

	Context("when hijacking succeeds", func() {
		BeforeEach(func() {
			scheduler.HijackStub = func(guid string, process warden.ProcessSpec, io warden.ProcessIO) error {
				Ω(io.Stdin).ShouldNot(BeZero())
				Ω(io.Stdout).ShouldNot(BeZero())
				Ω(io.Stderr).ShouldNot(BeZero())

				line, err := bufio.NewReader(io.Stdin).ReadBytes('\n')
				Ω(err).ShouldNot(HaveOccurred())
				Ω(string(line)).Should(Equal("hello server\n"))

				_, err = fmt.Fprintf(io.Stdout, "hello client out\n")
				Ω(err).ShouldNot(HaveOccurred())

				_, err = fmt.Fprintf(io.Stderr, "hello client err\n")
				Ω(err).ShouldNot(HaveOccurred())

				return nil
			}
		})

		It("hijacks the build via the scheduler", func() {
			_, err := fmt.Fprintf(conn, "hello server\n")
			Ω(err).ShouldNot(HaveOccurred())

			line, err := br.ReadBytes('\n')
			Ω(err).ShouldNot(HaveOccurred())
			Ω(string(line)).Should(Equal("hello client out\n"))

			line, err = br.ReadBytes('\n')
			Ω(err).ShouldNot(HaveOccurred())
			Ω(string(line)).Should(Equal("hello client err\n"))

			guid, spec, _ := scheduler.HijackArgsForCall(0)
			Ω(guid).Should(Equal("some-build-guid"))
			Ω(spec).Should(Equal(warden.ProcessSpec{
				Path: "bash",
				Args: []string{"-l"},
			}))
		})
	})
})
