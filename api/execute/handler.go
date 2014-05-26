package execute

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/scheduler"
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
	var build builds.Build
	err := json.NewDecoder(r.Body).Decode(&build)
	if err != nil {
		log.Println("malformed request:", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	err = handler.validateBuild(build)
	if err != nil {
		log.Println("invalid request:", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	log.Println("scheduling", build.Guid)

	handler.scheduler.Start(build)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(build)
}

func (handler *handler) validateBuild(build builds.Build) error {
	if build.Callback != "" {
		_, err := url.ParseRequestURI(build.Callback)
		if err != nil {
			return err
		}
	}

	return nil
}
