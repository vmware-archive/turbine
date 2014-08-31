package api

import (
	"net/http"

	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/rata"

	"github.com/concourse/turbine/api/abort"
	"github.com/concourse/turbine/api/check"
	"github.com/concourse/turbine/api/execute"
	"github.com/concourse/turbine/api/hijack"
	"github.com/concourse/turbine/resource"
	"github.com/concourse/turbine/routes"
	"github.com/concourse/turbine/scheduler"
)

func New(
	logger lager.Logger,
	scheduler scheduler.Scheduler,
	tracker resource.Tracker,
	turbineEndpoint *rata.RequestGenerator,
	drain <-chan struct{},
) (http.Handler, error) {
	checkHandler := check.NewHandler(logger, tracker, drain)

	handlers := map[string]http.Handler{
		routes.ExecuteBuild:     execute.NewHandler(logger, scheduler, turbineEndpoint),
		routes.AbortBuild:       abort.NewHandler(scheduler),
		routes.HijackBuild:      hijack.NewHandler(logger, scheduler),
		routes.CheckInput:       checkHandler,
		routes.CheckInputStream: http.HandlerFunc(checkHandler.Stream),
	}

	return rata.NewRouter(routes.Routes, handlers)
}
