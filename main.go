package main

import (
	"encoding/json"
	"flag"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	GardenClient "github.com/cloudfoundry-incubator/garden/client"
	GardenConnection "github.com/cloudfoundry-incubator/garden/client/connection"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/http_server"
	"github.com/tedsuo/ifrit/sigmon"
	"github.com/tedsuo/rata"

	"github.com/concourse/turbine/api"
	"github.com/concourse/turbine/builder"
	"github.com/concourse/turbine/builder/inputs"
	"github.com/concourse/turbine/builder/outputs"
	"github.com/concourse/turbine/config"
	"github.com/concourse/turbine/event"
	"github.com/concourse/turbine/resource"
	"github.com/concourse/turbine/routes"
	"github.com/concourse/turbine/scheduler"
	"github.com/concourse/turbine/snapshotter"
)

var listenAddr = flag.String(
	"listenAddr",
	"0.0.0.0:4637",
	"listening address",
)

var peerAddr = flag.String(
	"peerAddr",
	"127.0.0.1:4637",
	"external address of the api server, used for callbacks",
)

var debugListenAddr = flag.String(
	"debugListenAddr",
	":4636",
	"port for the pprof debugger to listen on",
)

var gardenNetwork = flag.String(
	"gardenNetwork",
	"tcp",
	"garden API connection network (unix or tcp)",
)

var gardenAddr = flag.String(
	"gardenAddr",
	"127.0.0.1:7777",
	"garden API connection address",
)

var resourceTypes = flag.String(
	"resourceTypes",
	`{
		"git": "docker:///concourse/git-resource",
		"archive": "docker:///concourse/archive-resource",
		"docker-image": "docker:///concourse/docker-image-resource",
		"time": "docker:///concourse/time-resource",
		"s3": "docker:///concourse/s3-resource",
		"tracker": "docker:///concourse/tracker-resource",
		"semver": "docker:///concourse/semver-resource"
	}`,
	"map of resource type to its docker image",
)

var snapshotPath = flag.String(
	"snapshotPath",
	"/tmp/builds-snapshot.json",
	"path to file to store/load snapshots from",
)

func main() {
	flag.Parse()

	gardenClient := GardenClient.New(GardenConnection.New(
		*gardenNetwork,
		*gardenAddr,
	))

	logger := lager.NewLogger("turbine")
	logger.RegisterSink(lager.NewWriterSink(os.Stdout, lager.DEBUG))

	resourceTypesMap := map[string]string{}
	err := json.Unmarshal([]byte(*resourceTypes), &resourceTypesMap)
	if err != nil {
		logger.Fatal("failed-to-parse-resource-types", err)
	}

	var resourceTypesConfig config.ResourceTypes
	for typ, image := range resourceTypesMap {
		resourceTypesConfig = append(resourceTypesConfig, config.ResourceType{
			Name:  typ,
			Image: image,
		})
	}

	resourceTracker := resource.NewTracker(resourceTypesConfig, gardenClient)

	builder := builder.NewBuilder(
		gardenClient,
		inputs.NewParallelFetcher(resourceTracker),
		outputs.NewParallelPerformer(resourceTracker),
	)

	scheduler := scheduler.NewScheduler(logger.Session("scheduler"), builder, event.NewWebSocketEmitter, scheduler.NewClock())

	generator := rata.NewRequestGenerator("http://"+*peerAddr, routes.Routes)

	drain := make(chan struct{})

	handler, err := api.New(logger.Session("api"), scheduler, resourceTracker, generator, drain)
	if err != nil {
		logger.Fatal("failed-to-initialize-handler", err)
	}

	var pingErr error
	for i := 0; i < 10; i++ {
		pingErr = gardenClient.Ping()
		if pingErr == nil {
			break
		}

		time.Sleep(10 * time.Second)
	}

	if pingErr != nil {
		logger.Fatal("failed-to-ping-garden", err)
	}

	group := grouper.NewParallel(os.Interrupt, []grouper.Member{
		{"api", http_server.New(*listenAddr, handler)},
		{"debug", http_server.New(*debugListenAddr, http.DefaultServeMux)},
		{"snapshotter", snapshotter.NewSnapshotter(logger.Session("snapshotter"), *snapshotPath, scheduler)},
		{"drainer", &drainer{drain}},
	})

	running := ifrit.Envoke(sigmon.New(group))

	logger.Info("listening", lager.Data{
		"api": *listenAddr,
	})

	err = <-running.Wait()
	if err == nil {
		logger.Info("exited")
	} else {
		logger.Error("failed", err)
		os.Exit(1)
	}
}

type drainer struct {
	drain chan<- struct{}
}

func (drainer *drainer) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	close(ready)
	<-signals
	close(drainer.drain)
	return nil
}
