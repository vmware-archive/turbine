package api

import (
	"net/http"

	"github.com/tedsuo/router"

	"github.com/winston-ci/prole/api/abort"
	"github.com/winston-ci/prole/api/check"
	"github.com/winston-ci/prole/api/execute"
	"github.com/winston-ci/prole/resource"
	"github.com/winston-ci/prole/routes"
	"github.com/winston-ci/prole/scheduler"
)

func New(
	scheduler scheduler.Scheduler,
	tracker resource.Tracker,
	proleEndpoint *router.RequestGenerator,
) (http.Handler, error) {
	handlers := map[string]http.Handler{
		routes.ExecuteBuild: execute.NewHandler(scheduler, proleEndpoint),
		routes.AbortBuild:   abort.NewHandler(scheduler),
		routes.CheckInput:   check.NewHandler(tracker),
	}

	return router.NewRouter(routes.Routes, handlers)
}
