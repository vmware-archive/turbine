package event_test

import (
	"encoding/json"
	"time"

	"github.com/concourse/turbine"
	. "github.com/concourse/turbine/event"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Encoding & Decoding messages", func() {
	var event Event

	itEncodesAndDecodesToItself := func() {
		It("encodes and decodes to itself", func() {
			payload, err := json.Marshal(Message{event})
			Ω(err).ShouldNot(HaveOccurred())

			var decodedMsg Message
			err = json.Unmarshal(payload, &decodedMsg)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(decodedMsg.Event).Should(Equal(event))
		})
	}

	Describe("Log", func() {
		BeforeEach(func() {
			event = Log{
				Payload: "some-payload",
				Origin: Origin{
					Type: OriginTypeInput,
					Name: "some-input",
				},
			}
		})

		itEncodesAndDecodesToItself()
	})

	Describe("Status", func() {
		BeforeEach(func() {
			event = Status{
				Status: turbine.StatusSucceeded,
				Time:   time.Now().Unix(),
			}
		})

		itEncodesAndDecodesToItself()
	})

	Describe("Initialize", func() {
		BeforeEach(func() {
			event = Initialize{
				BuildConfig: turbine.Config{
					Image: "some-image",
				},
			}
		})

		itEncodesAndDecodesToItself()
	})

	Describe("Start", func() {
		BeforeEach(func() {
			event = Start{
				Time: time.Now().Unix(),
			}
		})

		itEncodesAndDecodesToItself()
	})

	Describe("Finish", func() {
		BeforeEach(func() {
			event = Finish{
				Time:       time.Now().Unix(),
				ExitStatus: 42,
			}
		})

		itEncodesAndDecodesToItself()
	})

	Describe("Error", func() {
		BeforeEach(func() {
			event = Error{
				Message: "oh no!",
				Origin: Origin{
					Type: OriginTypeInput,
					Name: "some-input",
				},
			}
		})

		itEncodesAndDecodesToItself()
	})

	Describe("Input", func() {
		BeforeEach(func() {
			event = Input{
				Input: turbine.Input{
					Name: "some-resource",
				},
			}
		})

		itEncodesAndDecodesToItself()
	})

	Describe("Output", func() {
		BeforeEach(func() {
			event = Output{
				Output: turbine.Output{
					Name: "some-resource",
				},
			}
		})

		itEncodesAndDecodesToItself()
	})

	Describe("Version", func() {
		BeforeEach(func() {
			event = Version("1.0")
		})

		itEncodesAndDecodesToItself()
	})

	Describe("End", func() {
		BeforeEach(func() {
			event = End{}
		})

		itEncodesAndDecodesToItself()
	})
})
