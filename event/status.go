package event

import "github.com/concourse/turbine"

type Status struct {
	Status turbine.Status `json:"status"`
	Time   int64          `json:"time"`
}

func (Status) EventType() EventType { return EventTypeStatus }
