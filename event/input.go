package event

import "github.com/concourse/turbine"

type Input struct {
	Input turbine.Input `json:"input"`
}

func (Input) EventType() EventType { return EventTypeInput }
