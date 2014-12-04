package deletebuild

import (
	"net/http"

	"github.com/concourse/turbine/scheduler"
	"github.com/pivotal-golang/lager"
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

	hLog := handler.logger.Session("delete-build", lager.Data{
		"guid": guid,
	})

	hLog.Info("deleting")

	handler.scheduler.Delete(guid)

	w.WriteHeader(http.StatusNoContent)
}
