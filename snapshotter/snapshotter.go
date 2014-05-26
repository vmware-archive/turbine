package snapshotter

import (
	"encoding/json"
	"errors"
	"log"
	"os"

	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/builder"
	"github.com/winston-ci/prole/scheduler"
)

var ErrInvalidSnapshot = errors.New("invalid snapshot")

type Snapshotter struct {
	snapshotPath string
	scheduler    scheduler.Scheduler
}

type BuildSnapshot struct {
	Build           builds.Build `json:"build"`
	ContainerHandle string       `json:"handle"`
	ProcessID       uint32       `json:"process_id"`
}

func NewSnapshotter(snapshotPath string, scheduler scheduler.Scheduler) *Snapshotter {
	return &Snapshotter{
		snapshotPath: snapshotPath,
		scheduler:    scheduler,
	}
}

func (snapshotter *Snapshotter) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	snapshotFile, err := os.Open(snapshotter.snapshotPath)
	if err == nil {
		defer snapshotFile.Close()

		log.Println("restoring from", snapshotter.snapshotPath)

		var snapshots []BuildSnapshot
		err := json.NewDecoder(snapshotFile).Decode(&snapshots)
		if err != nil {
			return ErrInvalidSnapshot
		}

		for _, snapshot := range snapshots {
			go snapshotter.scheduler.Attach(builder.RunningBuild{
				Build:           snapshot.Build,
				ContainerHandle: snapshot.ContainerHandle,
				ProcessID:       snapshot.ProcessID,
			})
		}
	}

	close(ready)

	<-signals

	log.Println("draining...")

	running := snapshotter.scheduler.Drain()

	snapshotFile, err = os.Create(snapshotter.snapshotPath)
	if err != nil {
		return err
	}

	var snapshots []BuildSnapshot
	for _, running := range running {
		snapshots = append(snapshots, BuildSnapshot{
			Build:           running.Build,
			ContainerHandle: running.ContainerHandle,
			ProcessID:       running.ProcessID,
		})
	}

	err = json.NewEncoder(snapshotFile).Encode(&snapshots)
	if err != nil {
		return err
	}

	return snapshotFile.Close()
}
