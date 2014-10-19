package event

type Version string

func (Version) EventType() EventType { return EventTypeVersion }
