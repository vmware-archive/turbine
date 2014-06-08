package check

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/resource"
)

type handler struct {
	tracker resource.Tracker
}

func NewHandler(tracker resource.Tracker) http.Handler {
	return &handler{
		tracker: tracker,
	}
}

func (handler *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var input builds.Input
	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		log.Println("malformed request:", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	log.Println("checking", input)

	resource, err := handler.tracker.Init(input.Type, nil, nil)
	if err != nil {
		log.Println("checking failed:", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	defer handler.tracker.Release(resource)

	versions, err := resource.Check(input)
	if err != nil {
		log.Println("checking failed:", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(versions)
}
