package event

type End struct{}

func (End) EventType() EventType { return EventTypeEnd }
