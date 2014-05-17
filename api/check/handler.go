package check

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/checker"
)

type handler struct {
	checker checker.Checker
}

func NewHandler(checker checker.Checker) http.Handler {
	return &handler{
		checker: checker,
	}
}

func (handler *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var input builds.Input
	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		log.Println("malformed request:", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	log.Println("checking", input)

	versions, err := handler.checker.Check(input)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(versions)
}
