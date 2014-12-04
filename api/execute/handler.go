package execute

import (
	"encoding/json"
	"net/http"

	"github.com/nu7hatch/gouuid"
	"github.com/pivotal-golang/lager"

	"github.com/concourse/turbine"
	"github.com/concourse/turbine/scheduler"
)

type handler struct {
	logger lager.Logger

	scheduler       scheduler.Scheduler
	turbineEndpoint string
}

func NewHandler(
	logger lager.Logger,
	scheduler scheduler.Scheduler,
	turbineEndpoint string,
) http.Handler {
	return &handler{
		logger: logger,

		scheduler:       scheduler,
		turbineEndpoint: turbineEndpoint,
	}
}

func (handler *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var build turbine.Build
	err := json.NewDecoder(r.Body).Decode(&build)
	if err != nil {
		handler.logger.Error("malformed-request", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	log := handler.logger.Session("execute", lager.Data{
		"guid": build.Guid,
	})

	guid, err := uuid.NewV4()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	build.Guid = guid.String()

	log.Info("scheduling", lager.Data{
		"build": build,
	})

	handler.scheduler.Start(build)

	w.Header().Add("X-Turbine-Endpoint", handler.turbineEndpoint)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(build)
}
