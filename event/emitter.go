package event

const CURRENT_VERSION Version = "1.0"

type Emitter interface {
	EmitEvent(Event)
	Close()
}

type Version string
