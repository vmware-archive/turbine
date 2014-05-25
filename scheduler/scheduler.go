package scheduler

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/builder"
)

type Scheduler interface {
	Schedule(builds.Build) error
}

type scheduler struct {
	builder       builder.Builder
	runningBuilds *sync.WaitGroup

	httpClient *http.Client
}

func NewScheduler(builder builder.Builder) Scheduler {
	return &scheduler{
		builder: builder,

		httpClient: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},

		runningBuilds: new(sync.WaitGroup),
	}
}

func (scheduler *scheduler) Schedule(build builds.Build) error {
	scheduler.runningBuilds.Add(1)

	log.Printf("building: %#v\n", build)

	started, errored := scheduler.builder.Build(build)

	go scheduler.runBuild(build, started, errored)

	return nil
}

func (scheduler *scheduler) runBuild(originalBuild builds.Build, started <-chan builder.RunningBuild, errored <-chan error) {
	defer scheduler.runningBuilds.Done()

	var running builder.RunningBuild

	select {
	case running = <-started:
		log.Println("started")

		running.Build.Status = builds.StatusStarted
		scheduler.reportBuild(running.Build)
	case err := <-errored:
		log.Println("errored while starting:", err)

		originalBuild.Status = builds.StatusErrored
		scheduler.reportBuild(originalBuild)
		return
	}

	finished, failed, errored := scheduler.builder.Attach(running)

	select {
	case err := <-failed:
		log.Println("failed:", err)

		running.Build.Status = builds.StatusFailed
		scheduler.reportBuild(running.Build)
	case finishedBuild := <-finished:
		log.Println("completed")

		finishedBuild.Status = builds.StatusSucceeded
		scheduler.reportBuild(finishedBuild)
	case err := <-errored:
		log.Println("errored:", err)

		running.Build.Status = builds.StatusErrored
		scheduler.reportBuild(running.Build)
	}
}

func (scheduler *scheduler) reportBuild(build builds.Build) {
	if build.Callback == "" {
		return
	}

	// this should always successfully parse (it's done via validation)
	destination, _ := url.ParseRequestURI(build.Callback)

	payload, _ := json.Marshal(build)

	for {
		res, err := scheduler.httpClient.Do(&http.Request{
			Method: "PUT",
			URL:    destination,

			ContentLength: int64(len(payload)),

			Header: map[string][]string{
				"Content-Type": {"application/json"},
			},

			Body: ioutil.NopCloser(bytes.NewBuffer(payload)),
		})

		if err != nil {
			log.Println("failed to submit result:", err)
			time.Sleep(time.Second)
			continue
		}

		res.Body.Close()

		break
	}
}
