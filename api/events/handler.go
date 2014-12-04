package events

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/concourse/turbine/scheduler"
	"github.com/pivotal-golang/lager"
	"github.com/vito/go-sse/sse"
)

type handler struct {
	logger    lager.Logger
	scheduler scheduler.Scheduler
}

func NewHandler(logger lager.Logger, scheduler scheduler.Scheduler) http.Handler {
	return &handler{
		logger:    logger,
		scheduler: scheduler,
	}
}

func (handler *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	guid := r.FormValue(":guid")

	hLog := handler.logger.Session("build-events", lager.Data{
		"guid": guid,
	})

	var idx uint = 0

	lastID := r.Header.Get("Last-Event-ID")
	if lastID != "" {
		_, err := fmt.Sscanf(lastID, "%d", &idx)
		if err != nil {
			hLog.Error("bad-id", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		idx++
	}

	hLog.Info("subscribe", lager.Data{"last-event-id": idx})

	events, stop, err := handler.scheduler.Subscribe(guid, idx)
	if err != nil {
		hLog.Error("failed-to-subscribe", err)
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
