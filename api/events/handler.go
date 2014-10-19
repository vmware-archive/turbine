package events

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/concourse/turbine/scheduler"
	"github.com/vito/go-sse/sse"
)

type handler struct {
	scheduler scheduler.Scheduler
}

func NewHandler(scheduler scheduler.Scheduler) http.Handler {
	return &handler{
		scheduler: scheduler,
	}
}

func (handler *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	guid := r.FormValue(":guid")

	var idx uint = 0

	lastID := r.Header.Get("Last-Event-ID")
	if lastID != "" {
		_, err := fmt.Sscanf(lastID, "%d", &idx)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}

	events, stop, err := handler.scheduler.Subscribe(guid, idx)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	defer close(stop)

	flusher := w.(http.Flusher)
	closed := w.(http.CloseNotifier).CloseNotify()

	w.Header().Add("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Add("Connection", "keep-alive")

	w.WriteHeader(http.StatusOK)

	flusher.Flush()

	for {
		sseEvent := sse.Event{
			ID: fmt.Sprintf("%d", idx),
		}

		select {
		case e, ok := <-events:
			if !ok {
				return
			}

			data, err := json.Marshal(e)
			if err != nil {
				return
			}

			sseEvent.Name = string(e.EventType())
			sseEvent.Data = data

		case <-closed:
			return
		}

		err = sseEvent.Write(w)
		if err != nil {
			return
		}

		flusher.Flush()

		idx++
	}
}
