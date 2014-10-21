package event

const CURRENT_VERSION Version = "1.1"

type Emitter interface {
	EmitEvent(Event)
	Close()
}
