package api

import (
	"net/http"

	"github.com/tedsuo/router"

	"github.com/winston-ci/prole/api/check"
	"github.com/winston-ci/prole/api/execute"
	"github.com/winston-ci/prole/checker"
	"github.com/winston-ci/prole/routes"
	"github.com/winston-ci/prole/scheduler"
)

func New(scheduler scheduler.Scheduler, checker checker.Checker) (http.Handler, error) {
	handlers := map[string]http.Handler{
		routes.ExecuteBuild: execute.NewHandler(scheduler),
		routes.CheckInput:   check.NewHandler(checker),
	}

	return router.NewRouter(routes.Routes, handlers)
}
