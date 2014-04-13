package api

import (
	"log"
	"net/http"

	"github.com/rcrowley/go-tigertonic"

	"github.com/room101-ci/agent/api/builds"
)

func New(logger *log.Logger, buildScheduler builds.Scheduler) http.Handler {
	mux := tigertonic.NewTrieServeMux()

	builds := builds.NewHandler(buildScheduler)

	mux.Handle("POST", "/builds", logged(logger, builds.PostHandler()))

	return mux
}

func logged(logger *log.Logger, handler http.Handler) http.Handler {
	logged := tigertonic.Logged(handler, nil)
	logged.Logger = logger
	return logged
}
