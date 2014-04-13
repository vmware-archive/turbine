package api

import (
	"log"
	"net/http"

	"github.com/rcrowley/go-tigertonic"

	"github.com/room101-ci/agent/api/builds"
)

func New(logger *log.Logger) http.Handler {
	mux := tigertonic.NewTrieServeMux()

	builds := builds.NewHandler()

	mux.Handle("GET", "/builds/{guid}", logged(logger, builds.GetHandler()))

	return mux
}

func logged(logger *log.Logger, handler http.Handler) http.Handler {
	logged := tigertonic.Logged(handler, nil)
	logged.Logger = logger
	return logged
}
