package check

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/resource"
)

type Handler struct {
	tracker resource.Tracker
	drain   <-chan struct{}
}

func NewHandler(tracker resource.Tracker, drain <-chan struct{}) *Handler {
	return &Handler{
		tracker: tracker,
		drain:   drain,
	}
}

func (handler *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var interval time.Duration

	intervalStr := r.FormValue("interval")

	if intervalStr != "" {
		var err error

		interval, err = time.ParseDuration(intervalStr)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}
	}

	var input builds.Input
	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		log.Println("malformed request:", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	log.Printf("checking %s (type: %s)\n", input.Name, input.Type)

	resource, err := handler.tracker.Init(input.Type, nil, nil)
	if err != nil {
		log.Println("checking failed:", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	defer handler.tracker.Release(resource)

	flusher := w.(http.Flusher)

	encoder := json.NewEncoder(w)

	wroteHeader := false

	for {
		versions, err := resource.Check(input)
		if err != nil {
			if !wroteHeader {
				w.WriteHeader(http.StatusInternalServerError)
			}

			log.Println("checking failed:", err)
			w.Write([]byte(err.Error()))
			return
		}

		err = encoder.Encode(versions)
		if err != nil {
			log.Println("writing check result failed:", err)
			break
		}

		flusher.Flush()

		wroteHeader = true

		if len(versions) > 0 {
			break
		}

		if interval > 0 {
			time.Sleep(interval)
		} else {
			break
		}
	}
}
