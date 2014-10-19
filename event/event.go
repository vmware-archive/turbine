package event

type Event interface {
	EventType() EventType
}

type EventType string

const (
	EventTypeInvalid    EventType = ""
	EventTypeVersion    EventType = "version"
	EventTypeLog        EventType = "log"
	EventTypeStatus     EventType = "status"
	EventTypeInitialize EventType = "initialize"
	EventTypeStart      EventType = "start"
	EventTypeFinish     EventType = "finish"
	EventTypeError      EventType = "error"
	EventTypeInput      EventType = "input"
	EventTypeOutput     EventType = "output"
)
