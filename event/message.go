package event

import (
	"encoding/json"
	"fmt"
)

type Message struct {
	Event Event `json:"-"`
}

type eventEnvelope struct {
	Type         EventType        `json:"type"`
	EventPayload *json.RawMessage `json:"event"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	var envelope eventEnvelope

	payload, err := json.Marshal(m.Event)
	if err != nil {
		return nil, err
	}

	envelope.Type = m.Event.EventType()
	envelope.EventPayload = (*json.RawMessage)(&payload)

	return json.Marshal(envelope)
}

func (m *Message) UnmarshalJSON(bytes []byte) error {
	var envelope eventEnvelope

	err := json.Unmarshal(bytes, &envelope)
	if err != nil {
		return err
	}

	event, err := ParseEvent(envelope.Type, *envelope.EventPayload)
	if err != nil {
		return err
	}

	m.Event = event

	return nil
}

func ParseEvent(t EventType, payload []byte) (Event, error) {
	var ev Event
	var err error

	switch t {
	case EventTypeLog:
		event := Log{}
		err = json.Unmarshal(payload, &event)
		ev = event
	case EventTypeStatus:
		event := Status{}
		err = json.Unmarshal(payload, &event)
		ev = event
	case EventTypeInitialize:
		event := Initialize{}
		err = json.Unmarshal(payload, &event)
		ev = event
	case EventTypeStart:
		event := Start{}
		err = json.Unmarshal(payload, &event)
		ev = event
	case EventTypeFinish:
		event := Finish{}
		err = json.Unmarshal(payload, &event)
		ev = event
	case EventTypeError:
		event := Error{}
		err = json.Unmarshal(payload, &event)
		ev = event
	case EventTypeInput:
		event := Input{}
		err = json.Unmarshal(payload, &event)
		ev = event
	case EventTypeOutput:
		event := Output{}
		err = json.Unmarshal(payload, &event)
		ev = event
	case EventTypeVersion:
		event := Version("")
		err = json.Unmarshal(payload, &event)
		ev = event
	default:
		return nil, fmt.Errorf("unknown event type: %v", t)
	}

	return ev, err
}
