package hijack

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cloudfoundry-incubator/garden/warden"
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

	var spec warden.ProcessSpec
	err := json.NewDecoder(r.Body).Decode(&spec)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)

	conn, br, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}

	defer conn.Close()

	err = handler.scheduler.Hijack(guid, spec, warden.ProcessIO{
		Stdin:  br,
		Stdout: conn,
		Stderr: conn,
	})
	if err != nil {
		fmt.Fprintf(conn, "error: %s\n", err)
		return
	}
}
