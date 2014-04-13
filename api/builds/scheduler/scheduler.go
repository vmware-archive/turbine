package scheduler

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"

	"github.com/room101-ci/agent/api/builds"
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
			Transport: &http.Transport{},
		},

		runningBuilds: new(sync.WaitGroup),
	}
}

func (scheduler *Scheduler) Schedule(build *builds.Build) error {
	scheduler.runningBuilds.Add(1)

	go func() {
		defer scheduler.runningBuilds.Done()

		ok, err := scheduler.builder.Build(build)
		scheduler.completeBuild(*build, ok, err)
	}()

	return nil
}

func (scheduler *Scheduler) completeBuild(build builds.Build, ok bool, err error) {
	if err != nil {
		build.Status = "errored"
	} else if ok {
		build.Status = "succeeded"
	} else {
		build.Status = "failed"
	}

	destination, err := url.Parse(build.Callback)
	if err != nil {
		// this should be prevented by validation upfront
		panic("invalid build callback URL: " + build.Callback)
	}

	payload, err := json.Marshal(build)
	if err != nil {
		panic("failed to marshal build: " + err.Error())
	}

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
