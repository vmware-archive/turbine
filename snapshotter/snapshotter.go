package snapshotter

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/concourse/turbine/api/builds"
	"github.com/concourse/turbine/event"
	"github.com/concourse/turbine/scheduler"
	"github.com/pivotal-golang/lager"
)

var ErrInvalidSnapshot = errors.New("invalid snapshot")

type Snapshotter struct {
	logger lager.Logger

	snapshotPath string
	scheduler    scheduler.Scheduler
}

type BuildSnapshot struct {
	Build     builds.Build    `json:"build"`
	Status    builds.Status   `json:"status"`
	ProcessID uint32          `json:"process_id"`
	Events    []event.Message `json:"events"`
}

func NewSnapshotter(logger lager.Logger, snapshotPath string, scheduler scheduler.Scheduler) *Snapshotter {
	return &Snapshotter{
		logger: logger,

		snapshotPath: snapshotPath,
		scheduler:    scheduler,
	}
}

func (snapshotter *Snapshotter) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	log := snapshotter.logger.Session("run", lager.Data{
		"snapshot": snapshotter.snapshotPath,
	})

	snapshotFile, err := os.Open(snapshotter.snapshotPath)
	if err == nil {
		defer snapshotFile.Close()

		log.Info("snapshots-found")

		var snapshots []BuildSnapshot
		err := json.NewDecoder(snapshotFile).Decode(&snapshots)
		if err != nil {
			log.Error("malformed-snapshot", err)
		} else {
			for _, snapshot := range snapshots {
				log.Info("restoring", lager.Data{
					"snapshot": snapshot,
				})

				hub := event.NewHub()
				for _, m := range snapshot.Events {
					hub.EmitEvent(m.Event)
				}

				snapshotter.scheduler.Restore(scheduler.ScheduledBuild{
					Build:     snapshot.Build,
					Status:    snapshot.Status,
					ProcessID: snapshot.ProcessID,
					EventHub:  hub,
				})
			}
		}

		log.Info("restored")
	}

	close(ready)

	<-signals

	log.Info("draining")

	running := snapshotter.scheduler.Drain()

	snapshotFile, err = os.Create(snapshotter.snapshotPath)
	if err != nil {
		log.Error("failed-to-create-snapshot", err)
		return err
	}

	var snapshots []BuildSnapshot
	for _, running := range running {
		msgs := []event.Message{}
		for _, e := range running.EventHub.Events() {
			msgs = append(msgs, event.Message{e})
		}

		snapshots = append(snapshots, BuildSnapshot{
			Build:     running.Build,
			Status:    running.Status,
			ProcessID: running.ProcessID,
			Events:    msgs,
		})
	}

	err = json.NewEncoder(snapshotFile).Encode(&snapshots)
	if err != nil {
		log.Error("failed-to-encode-snapshot", err)
		return err
	}

	return snapshotFile.Close()
}
