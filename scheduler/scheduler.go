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

	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/builder"
)

type Scheduler interface {
	Start(builds.Build)
	Attach(builder.RunningBuild)
	Abort(guid string)

	Drain() []builder.RunningBuild
}

type scheduler struct {
	builder builder.Builder

	httpClient *http.Client

	inFlight *sync.WaitGroup
	draining chan struct{}
	running  map[*builder.RunningBuild]bool
	aborting map[string]chan struct{}

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
		aborting: make(map[string]chan struct{}),

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

	abort := scheduler.abortChannel(build.Guid)

	started, errored := scheduler.builder.Start(build, abort)

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

		scheduler.unregisterAbortChannel(build.Guid)
		scheduler.inFlight.Done()
	}()
}

func (scheduler *scheduler) Attach(running builder.RunningBuild) {
	scheduler.inFlight.Add(1) // in addition to .Start's
	defer scheduler.inFlight.Done()

	runningRef := &running

	scheduler.addRunning(runningRef)

	abort := scheduler.abortChannel(running.Build.Guid)
	defer scheduler.unregisterAbortChannel(running.Build.Guid)

	succeeded, failed, errored := scheduler.builder.Attach(running, abort)

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

func (scheduler *scheduler) Abort(guid string) {
	scheduler.abortBuild(guid)
}

func (scheduler *scheduler) complete(succeeded builder.SucceededBuild) {
	abort := scheduler.abortChannel(succeeded.Build.Guid)
	finished, errored := scheduler.builder.Complete(succeeded, abort)

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

func (scheduler *scheduler) abortChannel(guid string) chan struct{} {
	scheduler.mutex.Lock()
	defer scheduler.mutex.Unlock()

	abort, found := scheduler.aborting[guid]
	if !found {
		abort = make(chan struct{})
		scheduler.aborting[guid] = abort
	}

	return abort
}

func (scheduler *scheduler) unregisterAbortChannel(guid string) {
	scheduler.mutex.Lock()
	defer scheduler.mutex.Unlock()

	delete(scheduler.aborting, guid)
}

func (scheduler *scheduler) abortBuild(guid string) {
	scheduler.mutex.Lock()
	defer scheduler.mutex.Unlock()

	abort, found := scheduler.aborting[guid]
	if !found {
		return
	}

	close(abort)
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
