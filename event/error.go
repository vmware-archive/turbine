package event

type Error struct {
	Message string `json:"message"`
	Origin  Origin `json:"origin,omitempty"`
}

func (Error) EventType() EventType { return EventTypeError }
