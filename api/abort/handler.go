package abort

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

	handler.scheduler.Abort(guid)

	w.WriteHeader(http.StatusOK)
}
