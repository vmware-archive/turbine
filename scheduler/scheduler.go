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
)

type Builder interface {
	Build(*builds.Build) (bool, error)
}

type Scheduler struct {
	builder       Builder
	runningBuilds *sync.WaitGroup

	httpClient *http.Client
}

func NewScheduler(builder Builder) *Scheduler {
	return &Scheduler{
		builder: builder,

		httpClient: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},

		runningBuilds: new(sync.WaitGroup),
	}
}

func (scheduler *Scheduler) Schedule(build *builds.Build) error {
	scheduler.runningBuilds.Add(1)

	go func() {
		defer scheduler.runningBuilds.Done()

		log.Println("building", build.Guid)

		ok, err := scheduler.builder.Build(build)
		scheduler.completeBuild(*build, ok, err)
	}()

	return nil
}

func (scheduler *Scheduler) completeBuild(build builds.Build, succeeded bool, errored error) {
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

	scheduler.httpClient.Do(&http.Request{
		Method: "PUT",
		URL:    destination,

		ContentLength: int64(len(payload)),

		Header: map[string][]string{
			"Content-Type": {"application/json"},
		},

		Body: ioutil.NopCloser(bytes.NewBuffer(payload)),
	})
}
