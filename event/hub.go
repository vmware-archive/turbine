package event

import "sync"

type Hub struct {
	events []*eventOccurrence

	closed bool
	lock   *sync.RWMutex
}

type eventOccurrence struct {
	event   Event
	version Version

	occurred chan struct{}
}

func NewHub() *Hub {
	return &Hub{
		lock: new(sync.RWMutex),
		events: []*eventOccurrence{
			&eventOccurrence{
				occurred: make(chan struct{}),
			},
		},
	}
}

func (h *Hub) EmitEvent(event Event) {
	h.emit(event, "")
}

func (h *Hub) EmitVersion(version Version) {
	h.emit(nil, version)
}

func (h *Hub) Close() {
	h.lock.Lock()
	defer h.lock.Unlock()

	if h.closed {
		return
	}

	h.closed = true

	occ := h.events[len(h.events)-1]
	close(occ.occurred)
}

func (h *Hub) Subscribe(from uint, events chan<- Event, versions chan<- Version, stop <-chan struct{}) {
	for i := from; ; i++ {
		h.lock.RLock()

		if uint(len(h.events)) <= i {
			// out of bounds
			h.lock.RUnlock()

			close(events)
			close(versions)
			return
		}

		occ := h.events[i]
		h.lock.RUnlock()

		select {
		case <-occ.occurred:
		case <-stop:
			return
		}

		if occ.version != "" {
			select {
			case versions <- occ.version:
			case <-stop:
				return
			}
		} else if occ.event != nil {
			select {
			case events <- occ.event:
			case <-stop:
				return
			}
		} else {
			// reached end of stream
			close(events)
			close(versions)
			return
		}
	}
}

func (h *Hub) emit(event Event, version Version) {
	h.lock.Lock()
	defer h.lock.Unlock()

	if h.closed {
		return
	}

	occ := h.events[len(h.events)-1]
	occ.event = event
	occ.version = version

	nextOcc := &eventOccurrence{
		occurred: make(chan struct{}),
	}

	h.events = append(h.events, nextOcc)

	close(occ.occurred)
}
