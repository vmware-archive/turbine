package event

import "github.com/concourse/turbine"

type Initialize struct {
	BuildConfig turbine.Config `json:"config"`
}

func (Initialize) EventType() EventType { return EventTypeInitialize }
