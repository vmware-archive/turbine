package event

type Log struct {
	Origin  Origin `json:"origin"`
	Payload string `json:"payload"`
}

func (Log) EventType() EventType { return EventTypeLog }
