package api

import (
	"net/http"

	"github.com/tedsuo/router"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/routes"
)

func New(scheduler builds.Scheduler) (http.Handler, error) {
	builds := builds.NewHandler(scheduler)

	handlers := map[string]http.Handler{
		routes.ExecuteBuild: http.HandlerFunc(builds.PostHandler),
	}

	return router.NewRouter(routes.Routes, handlers)
}
