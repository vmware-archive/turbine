package event

import "github.com/concourse/turbine"

type Output struct {
	Output turbine.Output `json:"output"`
}

func (Output) EventType() EventType { return EventTypeOutput }
