package execute

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"

	"github.com/nu7hatch/gouuid"
	"github.com/tedsuo/router"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/routes"
	"github.com/winston-ci/prole/scheduler"
)

type handler struct {
	scheduler     scheduler.Scheduler
	proleEndpoint *router.RequestGenerator
}

func NewHandler(
	scheduler scheduler.Scheduler,
	proleEndpoint *router.RequestGenerator,
) http.Handler {
	return &handler{
		scheduler:     scheduler,
		proleEndpoint: proleEndpoint,
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

	guid, err := uuid.NewV4()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	build.Guid = guid.String()

	abortReq, err := handler.proleEndpoint.RequestForHandler(
		routes.AbortBuild,
		router.Params{"guid": build.Guid},
		nil,
	)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	build.AbortURL = abortReq.URL.String()

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
