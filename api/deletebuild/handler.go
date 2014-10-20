package deletebuild

import (
	"net/http"

	"github.com/concourse/turbine/scheduler"
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
	guid := r.FormValue(":guid")

	handler.scheduler.Delete(guid)

	w.WriteHeader(http.StatusNoContent)
}
