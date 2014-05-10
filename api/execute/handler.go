package execute

import (
	"encoding/json"
	"errors"
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
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err = handler.validateBuild(build)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	log.Println("scheduling", build.Guid)

	err = handler.scheduler.Schedule(build)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(build)
}

func (handler *handler) validateBuild(build builds.Build) error {
	for _, source := range build.Sources {
		if source.Type == "git" && source.Branch == "" {
			return errors.New("missing build source branch")
		}

		if source.Type == "git" && source.Ref == "" {
			return errors.New("missing build source ref")
		}
	}

	if build.Callback != "" {
		_, err := url.ParseRequestURI(build.Callback)
		if err != nil {
			return err
		}
	}

	return nil
}
