package execute

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/nu7hatch/gouuid"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/rata"

	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/routes"
	"github.com/concourse/turbine/scheduler"
)

type handler struct {
	logger lager.Logger

	scheduler       scheduler.Scheduler
	turbineEndpoint *rata.RequestGenerator
}

func NewHandler(
	logger lager.Logger,
	scheduler scheduler.Scheduler,
	turbineEndpoint *rata.RequestGenerator,
) http.Handler {
	return &handler{
		logger: logger,

		scheduler:       scheduler,
		turbineEndpoint: turbineEndpoint,
	}
}

func (handler *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var build builds.Build
	err := json.NewDecoder(r.Body).Decode(&build)
	if err != nil {
		handler.logger.Error("malformed-request", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	log := handler.logger.Session("execute", lager.Data{
		"build": build,
	})

	err = handler.validateBuild(build)
	if err != nil {
		log.Error("invalid-request", err)
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

	abortReq, err := handler.turbineEndpoint.CreateRequest(
		routes.AbortBuild,
		rata.Params{"guid": build.Guid},
		nil,
	)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	build.AbortURL = abortReq.URL.String()

	hijackReq, err := handler.turbineEndpoint.CreateRequest(
		routes.HijackBuild,
		rata.Params{"guid": build.Guid},
		nil,
	)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	build.HijackURL = hijackReq.URL.String()

	log.Info("scheduling", lager.Data{
		"guid": build.Guid,
	})

	handler.scheduler.Start(build)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(build)
}

func (handler *handler) validateBuild(build builds.Build) error {
	if build.StatusCallback != "" {
		_, err := url.ParseRequestURI(build.StatusCallback)
		if err != nil {
			return err
		}
	}

	if build.EventsCallback != "" {
		_, err := url.ParseRequestURI(build.EventsCallback)
		if err != nil {
			return err
		}
	}

	return nil
}
