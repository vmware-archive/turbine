package testlog

import (
	"sync"

	"github.com/concourse/turbine/event"
)

type EventLog struct {
	events  []event.Event
	eventsL sync.RWMutex
}

func (l *EventLog) Add(e event.Event) {
	l.eventsL.Lock()
	l.events = append(l.events, e)
	l.eventsL.Unlock()
}

func (l *EventLog) Sent() []event.Event {
	l.eventsL.RLock()
	events := make([]event.Event, len(l.events))
	copy(events, l.events)
	l.eventsL.RUnlock()
	return events
}
