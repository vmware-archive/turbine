package event

type Event interface {
	EventType() EventType
}

type EventType string

const (
	EventTypeInvalid EventType = ""

	// event stream protocol version
	EventTypeVersion EventType = "version"

	// end of stream
	EventTypeEnd EventType = "end"

	// build log (e.g. from input or build execution)
	EventTypeLog EventType = "log"

	// build status change (e.g. 'started', 'succeeded')
	EventTypeStatus EventType = "status"

	// build initializing (all inputs fetched; fetching image)
	EventTypeInitialize EventType = "initialize"

	// build execution started
	EventTypeStart EventType = "start"

	// build execution finished
	EventTypeFinish EventType = "finish"

	// error occurred
	EventTypeError EventType = "error"

	// input fetched
	EventTypeInput EventType = "input"

	// output completed
	EventTypeOutput EventType = "output"
)
