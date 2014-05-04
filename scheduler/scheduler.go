package scheduler

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"sync"

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

	go func() {
		defer scheduler.runningBuilds.Done()

		log.Println("building", build.Guid)

		ok, err := scheduler.builder.Build(build)
		scheduler.completeBuild(build, ok, err)
	}()

	return nil
}

func (scheduler *scheduler) completeBuild(build builds.Build, succeeded bool, errored error) {
	if errored != nil {
		build.Status = "errored"
	} else if succeeded {
		build.Status = "succeeded"
	} else {
		build.Status = "failed"
	}

	log.Println("completed:", build.Guid, build.Status, errored)

	if build.Callback == "" {
		return
	}

	// this should always successfully parse (it's done via validation)
	destination, _ := url.ParseRequestURI(build.Callback)

	payload, _ := json.Marshal(build)

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
		return
	}

	res.Body.Close()
}
