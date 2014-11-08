package check

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/pivotal-golang/lager"

	"github.com/concourse/turbine"
	"github.com/concourse/turbine/resource"
)

type Handler struct {
	logger lager.Logger

	tracker resource.Tracker
	drain   <-chan struct{}
}

func NewHandler(logger lager.Logger, tracker resource.Tracker, drain <-chan struct{}) *Handler {
	return &Handler{
		logger:  logger,
		tracker: tracker,
		drain:   drain,
	}
}

func (handler *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var input turbine.Input
	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		handler.logger.Error("malformed-request", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	log := handler.logger.Session("check", lager.Data{
		"resource": input.Resource,
		"type":     input.Type,
	})

	log.Info("checking", lager.Data{
		"from": input.Version,
	})

	resource, err := handler.tracker.Init(input.Type, ioutil.Discard, nil)
	if err != nil {
		log.Error("failed-to-init", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	defer handler.tracker.Release(resource)

	versions, err := resource.Check(input)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Error("failed-to-check", err)
		w.Write([]byte(err.Error()))
		return
	}

	json.NewEncoder(w).Encode(versions)
}
