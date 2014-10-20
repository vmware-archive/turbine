package scheduler

import (
	"fmt"
	"net/http"
	"sync"

	gapi "github.com/cloudfoundry-incubator/garden/api"
	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/builder"
	"github.com/concourse/turbine/event"
	"github.com/pivotal-golang/lager"
)

type Scheduler interface {
	Start(builds.Build)
	Restore(ScheduledBuild)
	Abort(guid string)
	Hijack(guid string, process gapi.ProcessSpec, io gapi.ProcessIO) (gapi.Process, error)
	Subscribe(guid string, from uint) (<-chan event.Event, chan<- struct{}, error)

	Drain() []ScheduledBuild
}

type ScheduledBuild struct {
	Build     builds.Build
	Status    builds.Status
	ProcessID uint32
	EventHub  *event.Hub

	abort chan struct{}
}

type scheduler struct {
	logger lager.Logger

	builder builder.Builder

	clock Clock

	httpClient *http.Client

	inFlight *sync.WaitGroup
	draining chan struct{}

	builds map[string]*ScheduledBuild

	mutex *sync.RWMutex
}

func NewScheduler(
	l lager.Logger,
	b builder.Builder,
	clock Clock,
) Scheduler {
	return &scheduler{
		logger: l,

		builder: b,

		clock: clock,

		httpClient: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},

		inFlight: new(sync.WaitGroup),
		draining: make(chan struct{}),

		builds: make(map[string]*ScheduledBuild),

		mutex: new(sync.RWMutex),
	}
}

func (scheduler *scheduler) Drain() []ScheduledBuild {
	close(scheduler.draining)
	scheduler.inFlight.Wait()
	return scheduler.scheduledBuilds()
}

func (scheduler *scheduler) Start(build builds.Build) {
	scheduler.inFlight.Add(1)

	log := scheduler.logger.Session("start", lager.Data{
		"build": build,
	})

	scheduled := &ScheduledBuild{
		Build:    build,
		EventHub: event.NewHub(),

		abort: make(chan struct{}),
	}

	scheduled.EventHub.EmitEvent(event.CURRENT_VERSION)

	scheduler.mutex.Lock()
	scheduler.builds[build.Guid] = scheduled
	scheduler.mutex.Unlock()

	go func() {
		defer scheduler.inFlight.Done()

		running, err := scheduler.builder.Start(scheduled.Build, scheduled.EventHub, scheduled.abort)
		if err != nil {
			log.Error("errored", err)

			select {
			case <-scheduled.abort:
				scheduler.updateAndReportBuild(build, builds.StatusAborted)
			default:
				scheduler.updateAndReportBuild(build, builds.StatusErrored)
			}
		} else {
			log.Info("started")

			scheduler.updateRunningBuild(running)
			scheduler.updateAndReportBuild(running.Build, builds.StatusStarted)

			scheduler.attach(running, scheduled)
		}
	}()
}

func (scheduler *scheduler) Restore(build ScheduledBuild) {
	scheduled := &build
	scheduled.abort = make(chan struct{})

	scheduler.mutex.Lock()
	scheduler.builds[scheduled.Build.Guid] = scheduled
	scheduler.mutex.Unlock()

	if build.Status == builds.StatusStarted {
		scheduled.EventHub.EmitEvent(event.CURRENT_VERSION)

		scheduler.inFlight.Add(1)

		go func() {
			defer scheduler.inFlight.Done()

			scheduler.attach(
				builder.RunningBuild{
					Build:     scheduled.Build,
					ProcessID: scheduled.ProcessID,
				},
				scheduled,
			)
		}()
	} else {
		scheduled.EventHub.Close()
	}
}

func (scheduler *scheduler) Abort(guid string) {
	scheduler.mutex.Lock()
	defer scheduler.mutex.Unlock()

	scheduled, found := scheduler.builds[guid]
	if !found {
		return
	}

	close(scheduled.abort)
}

func (scheduler *scheduler) Hijack(guid string, spec gapi.ProcessSpec, io gapi.ProcessIO) (gapi.Process, error) {
	return scheduler.builder.Hijack(guid, spec, io)
}

func (scheduler *scheduler) Subscribe(guid string, from uint) (<-chan event.Event, chan<- struct{}, error) {
	scheduler.mutex.RLock()
	scheduled, found := scheduler.builds[guid]
	scheduler.mutex.RUnlock()

	if !found {
		return nil, nil, fmt.Errorf("unknown build: %s", guid)
	}

	events := make(chan event.Event)
	stop := make(chan struct{})

	go scheduled.EventHub.Subscribe(from, events, stop)

	return events, stop, nil
}

func (scheduler *scheduler) attach(running builder.RunningBuild, scheduled *ScheduledBuild) {
	defer scheduled.EventHub.Close()

	log := scheduler.logger.Session("attach", lager.Data{
		"build": running.Build,
	})

	exited := make(chan builder.ExitedBuild, 1)
	errored := make(chan error, 1)

	go func() {
		ex, err := scheduler.builder.Attach(running, scheduled.EventHub, scheduled.abort)
		if err != nil {
			errored <- err
		} else {
			exited <- ex
		}
	}()

	select {
	case build := <-exited:
		log.Info("exited")

		scheduler.finish(build, scheduled)
	case err := <-errored:
		log.Error("errored", err)

		select {
		case <-scheduled.abort:
			scheduler.updateAndReportBuild(running.Build, builds.StatusAborted)
		default:
			scheduler.updateAndReportBuild(running.Build, builds.StatusErrored)
		}
	case <-scheduler.draining:
	}
}

func (scheduler *scheduler) finish(exited builder.ExitedBuild, scheduled *ScheduledBuild) {
	log := scheduler.logger.Session("finish", lager.Data{
		"build": exited.Build,
	})

	finished, err := scheduler.builder.Finish(exited, scheduled.EventHub, scheduled.abort)
	if err != nil {
		log.Error("failed", err)

		select {
		case <-scheduled.abort:
			scheduler.updateAndReportBuild(exited.Build, builds.StatusAborted)
		default:
			scheduler.updateAndReportBuild(exited.Build, builds.StatusErrored)
		}
	} else {
		log.Info("finished")

		if exited.ExitStatus == 0 {
			scheduler.updateAndReportBuild(finished, builds.StatusSucceeded)
		} else {
			scheduler.updateAndReportBuild(finished, builds.StatusFailed)
		}
	}
}

func (scheduler *scheduler) scheduledBuilds() []ScheduledBuild {
	scheduler.mutex.RLock()

	scheduled := []ScheduledBuild{}
	for _, build := range scheduler.builds {
		scheduled = append(scheduled, *build)
	}

	scheduler.mutex.RUnlock()

	return scheduled
}

func (scheduler *scheduler) updateRunningBuild(running builder.RunningBuild) {
	scheduler.mutex.Lock()
	scheduler.builds[running.Build.Guid].ProcessID = running.ProcessID
	scheduler.mutex.Unlock()
}

func (scheduler *scheduler) updateAndReportBuild(
	build builds.Build,
	status builds.Status,
) {
	scheduler.mutex.Lock()
	scheduled := scheduler.builds[build.Guid]
	scheduled.Status = status
	scheduler.mutex.Unlock()

	scheduled.EventHub.EmitEvent(event.Status{
		Status: scheduled.Status,
		Time:   scheduler.clock.CurrentTime().Unix(),
	})
}
