package event_test

import (
	"encoding/json"

	"github.com/concourse/turbine/api/builds"
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
				Payload: []byte("some-payload"),
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
				Status: builds.StatusSucceeded,
			}
		})

		itEncodesAndDecodesToItself()
	})
})
