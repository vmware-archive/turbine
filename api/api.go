package api

import (
	"net/http"

	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/rata"

	"github.com/concourse/turbine"
	"github.com/concourse/turbine/api/abort"
	"github.com/concourse/turbine/api/check"
	"github.com/concourse/turbine/api/deletebuild"
	"github.com/concourse/turbine/api/events"
	"github.com/concourse/turbine/api/execute"
	"github.com/concourse/turbine/api/hijack"
	"github.com/concourse/turbine/resource"
	"github.com/concourse/turbine/scheduler"
)

func New(
	logger lager.Logger,
	scheduler scheduler.Scheduler,
	tracker resource.Tracker,
	turbineEndpoint string,
	drain <-chan struct{},
) (http.Handler, error) {
	checkHandler := check.NewHandler(logger, tracker, drain)

	handlers := map[string]http.Handler{
		turbine.ExecuteBuild:     execute.NewHandler(logger, scheduler, turbineEndpoint),
		turbine.DeleteBuild:      deletebuild.NewHandler(scheduler),
		turbine.AbortBuild:       abort.NewHandler(scheduler),
		turbine.HijackBuild:      hijack.NewHandler(logger, scheduler),
		turbine.GetBuildEvents:   events.NewHandler(scheduler),
		turbine.CheckInput:       checkHandler,
		turbine.CheckInputStream: http.HandlerFunc(checkHandler.Stream),
	}

	return rata.NewRouter(turbine.Routes, handlers)
}
