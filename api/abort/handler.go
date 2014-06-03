package abort

import (
	"log"
	"net/http"

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
	guid := r.FormValue(":guid")

	log.Println("aborting", guid)

	handler.scheduler.Abort(guid)

	w.WriteHeader(http.StatusOK)
}
