package resource_test

import (
	"errors"
	"io/ioutil"

	garden "github.com/cloudfoundry-incubator/garden/api"
	gfakes "github.com/cloudfoundry-incubator/garden/api/fakes"
	"github.com/concourse/turbine"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Resource Check", func() {
	var (
		input turbine.Input

		checkScriptStdout     string
		checkScriptStderr     string
		checkScriptExitStatus int
		runCheckError         error

		checkScriptProcess *gfakes.FakeProcess

		checkResult []turbine.Version
		checkErr    error
	)

	BeforeEach(func() {
		input = turbine.Input{
			Type:    "some-resource",
			Source:  turbine.Source{"some": "source"},
			Version: turbine.Version{"some": "version"},
		}

		checkScriptStdout = "[]"
		checkScriptStderr = ""
		checkScriptExitStatus = 0
		runCheckError = nil

		checkScriptProcess = new(gfakes.FakeProcess)
		checkScriptProcess.WaitStub = func() (int, error) {
			return checkScriptExitStatus, nil
		}

		checkResult = nil
		checkErr = nil
	})

	JustBeforeEach(func() {
		gardenClient.Connection.RunStub = func(handle string, spec garden.ProcessSpec, io garden.ProcessIO) (garden.Process, error) {
			if runCheckError != nil {
				return nil, runCheckError
			}

			_, err := io.Stdout.Write([]byte(checkScriptStdout))
			Ω(err).ShouldNot(HaveOccurred())

			_, err = io.Stderr.Write([]byte(checkScriptStderr))
			Ω(err).ShouldNot(HaveOccurred())

			return checkScriptProcess, nil
		}

		checkResult, checkErr = resource.Check(input)
	})

	It("runs /opt/resource/check the request on stdin", func() {
		Ω(checkErr).ShouldNot(HaveOccurred())

		handle, spec, io := gardenClient.Connection.RunArgsForCall(0)
		Ω(handle).Should(Equal("some-handle"))
		Ω(spec.Path).Should(Equal("/opt/resource/check"))
		Ω(spec.Args).Should(BeEmpty())

		request, err := ioutil.ReadAll(io.Stdin)
		Ω(err).ShouldNot(HaveOccurred())

		Ω(string(request)).Should(Equal(`{"version":{"some":"version"},"source":{"some":"source"}}`))
	})

	Context("when /check outputs versions", func() {
		BeforeEach(func() {
			checkScriptStdout = `[{"ver":"abc"}, {"ver":"def"}, {"ver":"ghi"}]`
		})

		It("returns the raw parsed contents", func() {
			Ω(checkErr).ShouldNot(HaveOccurred())

			Ω(checkResult).Should(Equal([]turbine.Version{
				turbine.Version{"ver": "abc"},
				turbine.Version{"ver": "def"},
				turbine.Version{"ver": "ghi"},
			}))
		})
	})

	Context("when running /opt/resource/check fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			runCheckError = disaster
		})

		It("returns an err containing stdout/stderr of the process", func() {
			Ω(checkErr).Should(Equal(disaster))
		})
	})

	Context("when /opt/resource/check exits nonzero", func() {
		BeforeEach(func() {
			checkScriptStdout = "some-stdout-data"
			checkScriptStderr = "some-stderr-data"
			checkScriptExitStatus = 9
		})

		It("returns an err containing stdout/stderr of the process", func() {
			Ω(checkErr).Should(HaveOccurred())

			Ω(checkErr.Error()).Should(ContainSubstring("some-stdout-data"))
			Ω(checkErr.Error()).Should(ContainSubstring("some-stderr-data"))
			Ω(checkErr.Error()).Should(ContainSubstring("exit status 9"))
		})
	})

	Context("when the output of /opt/resource/check is malformed", func() {
		BeforeEach(func() {
			checkScriptStdout = "ß"
		})

		It("returns an error", func() {
			Ω(checkErr).Should(HaveOccurred())
		})
	})
})
