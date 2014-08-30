package event

type Event interface {
	EventType() EventType
}

type EventType int

const (
	EventTypeInvalid = EventType(iota)
	EventTypeLog
	EventTypeStatus
)
