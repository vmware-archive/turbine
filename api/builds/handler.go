package builds

import (
	"errors"
	"log"
	"net/http"
	"net/url"

	"github.com/rcrowley/go-tigertonic"
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

func (handler *Handler) PostHandler() http.Handler {
	return tigertonic.Marshaled(handler.post)
}

func (handler *Handler) post(url *url.URL, header http.Header, build *Build) (int, http.Header, *Build, error) {
	err := handler.validateBuild(build)
	if err != nil {
		return http.StatusBadRequest, nil, nil, err
	}

	log.Println("scheduling", build.Guid)

	err = handler.scheduler.Schedule(build)
	if err != nil {
		return http.StatusServiceUnavailable, nil, nil, err
	}

	return http.StatusCreated, nil, build, nil
}

func (handler *Handler) validateBuild(build *Build) error {
	if build.Guid == "" {
		return errors.New("missing build guid")
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
