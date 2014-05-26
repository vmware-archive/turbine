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
	Start(builds.Build)
	Attach(builder.RunningBuild)

	Drain() []builder.RunningBuild
}

type scheduler struct {
	builder builder.Builder

	httpClient *http.Client

	inFlight *sync.WaitGroup
	draining chan struct{}
	running  map[*builder.RunningBuild]bool

	mutex *sync.RWMutex
}

func NewScheduler(b builder.Builder) Scheduler {
	return &scheduler{
		builder: b,

		httpClient: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},

		inFlight: new(sync.WaitGroup),
		draining: make(chan struct{}),
		running:  make(map[*builder.RunningBuild]bool),

		mutex: new(sync.RWMutex),
	}
}

func (scheduler *scheduler) Drain() []builder.RunningBuild {
	close(scheduler.draining)
	scheduler.inFlight.Wait()
	return scheduler.runningBuilds()
}

func (scheduler *scheduler) Start(build builds.Build) {
	scheduler.inFlight.Add(1)

	log.Printf("building: %#v\n", build)

	started, errored := scheduler.builder.Start(build)

	go func() {
		select {
		case running := <-started:
			log.Println("started")

			running.Build.Status = builds.StatusStarted
			scheduler.reportBuild(running.Build)

			scheduler.Attach(running)
		case err := <-errored:
			log.Println("errored while starting:", err)

			build.Status = builds.StatusErrored
			scheduler.reportBuild(build)
		}

		scheduler.inFlight.Done()
	}()
}

func (scheduler *scheduler) Attach(running builder.RunningBuild) {
	runningRef := &running

	scheduler.addRunning(runningRef)

	succeeded, failed, errored := scheduler.builder.Attach(running)

	select {
	case err := <-failed:
		log.Println("failed:", err)

		running.Build.Status = builds.StatusFailed
		scheduler.reportBuild(running.Build)

	case succeededBuild := <-succeeded:
		log.Println("succeeded")

		scheduler.complete(succeededBuild)

	case err := <-errored:
		log.Println("errored:", err)

		running.Build.Status = builds.StatusErrored
		scheduler.reportBuild(running.Build)

	case <-scheduler.draining:
		return
	}

	scheduler.removeRunning(runningRef)
}

func (scheduler *scheduler) complete(succeeded builder.SucceededBuild) {
	finished, errored := scheduler.builder.Complete(succeeded)

	select {
	case finishedBuild := <-finished:
		log.Println("completed")

		finishedBuild.Status = builds.StatusSucceeded
		scheduler.reportBuild(finishedBuild)

	case err := <-errored:
		log.Println("failed to complete:", err)

		succeeded.Build.Status = builds.StatusErrored
		scheduler.reportBuild(succeeded.Build)
	}
}

func (scheduler *scheduler) runningBuilds() []builder.RunningBuild {
	scheduler.mutex.RLock()

	running := []builder.RunningBuild{}
	for build, _ := range scheduler.running {
		running = append(running, *build)
	}

	scheduler.mutex.RUnlock()

	return running
}

func (scheduler *scheduler) addRunning(running *builder.RunningBuild) {
	scheduler.mutex.Lock()
	scheduler.running[running] = true
	scheduler.mutex.Unlock()
}

func (scheduler *scheduler) removeRunning(running *builder.RunningBuild) {
	scheduler.mutex.Lock()
	delete(scheduler.running, running)
	scheduler.mutex.Unlock()
}

func (scheduler *scheduler) runBuild(originalBuild builds.Build, started <-chan builder.RunningBuild, errored <-chan error) {
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
