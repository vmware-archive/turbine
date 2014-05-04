package builds

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
)

type Scheduler interface {
	Schedule(*Build) error
}

type Handler struct {
	scheduler Scheduler
}

func NewHandler(scheduler Scheduler) *Handler {
	return &Handler{
		scheduler: scheduler,
	}
}

func (handler *Handler) PostHandler(w http.ResponseWriter, r *http.Request) {
	var build *Build
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

func (handler *Handler) validateBuild(build *Build) error {
	if build.Guid == "" {
		return errors.New("missing build guid")
	}

	if build.Source.Type == "git" && build.Source.Branch == "" {
		return errors.New("missing build source branch")
	}

	if build.Source.Type == "git" && build.Source.Ref == "" {
		return errors.New("missing build source ref")
	}

	if build.Callback != "" {
		_, err := url.ParseRequestURI(build.Callback)
		if err != nil {
			return err
		}
	}

	return nil
}
