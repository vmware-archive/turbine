package event

const VERSION = "1.0"

type Emitter interface {
	EmitEvent(Event)
	Close()
}

type NullEmitter struct{}

func (NullEmitter) EmitEvent(Event) {}
func (NullEmitter) Close()          {}
