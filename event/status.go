package event

import "github.com/concourse/turbine/api/builds"

type Status struct {
	Status builds.Status `json:"status"`
	Time   int64         `json:"time"`
}

func (Status) EventType() EventType { return EventTypeStatus }
