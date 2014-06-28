package main

import (
	"encoding/json"
	"flag"
	"os"

	WardenClient "github.com/cloudfoundry-incubator/garden/client"
	WardenConnection "github.com/cloudfoundry-incubator/garden/client/connection"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/http_server"
	"github.com/tedsuo/ifrit/sigmon"
	"github.com/tedsuo/router"

	"github.com/concourse/turbine/api"
	"github.com/concourse/turbine/builder"
	"github.com/concourse/turbine/config"
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

var wardenNetwork = flag.String(
	"wardenNetwork",
	"unix",
	"warden API connection network (unix or tcp)",
)

var wardenAddr = flag.String(
	"wardenAddr",
	"/tmp/warden.sock",
	"warden API connection address",
)

var resourceTypes = flag.String(
	"resourceTypes",
	`{"git":"concourse/git-resource","raw":"concourse/raw-resource","docker-image":"concourse/docker-image-resource","time":"concourse/time-resource"}`,
	"map of resource type to its docker image",
)

var snapshotPath = flag.String(
	"snapshotPath",
	"/tmp/builds-snapshot.json",
	"path to file to store/load snapshots from",
)

func main() {
	flag.Parse()

	wardenClient := WardenClient.New(WardenConnection.New(
		*wardenNetwork,
		*wardenAddr,
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

	resourceTracker := resource.NewTracker(resourceTypesConfig, wardenClient)

	builder := builder.NewBuilder(resourceTracker, wardenClient)

	scheduler := scheduler.NewScheduler(builder, logger.Session("scheduler"))

	generator := router.NewRequestGenerator("http://"+*peerAddr, routes.Routes)

	drain := make(chan struct{})

	handler, err := api.New(logger.Session("api"), scheduler, resourceTracker, generator, drain)
	if err != nil {
		logger.Fatal("failed-to-initialize-handler", err)
	}

	group := grouper.EnvokeGroup(grouper.RunGroup{
		"api":         http_server.New(*listenAddr, handler),
		"snapshotter": snapshotter.NewSnapshotter(logger.Session("snapshotter"), *snapshotPath, scheduler),
		"drainer": ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
			close(ready)
			<-signals
			close(drain)
			return nil
		}),
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
