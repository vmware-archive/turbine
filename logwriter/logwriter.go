package logwriter

import (
	"io"

	"github.com/concourse/turbine/event"
)

type writer struct {
	emitter event.Emitter
	origin  event.Origin
}

func NewWriter(emitter event.Emitter, origin event.Origin) io.Writer {
	return &writer{
		emitter: emitter,
		origin:  origin,
	}
}

func (writer *writer) Write(data []byte) (int, error) {
	writer.emitter.EmitEvent(event.Log{
		Payload: data,
		Origin:  writer.origin,
	})

	return len(data), nil
}
