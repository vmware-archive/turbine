package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"

	WardenClient "github.com/cloudfoundry-incubator/garden/client"
	WardenConnection "github.com/cloudfoundry-incubator/garden/client/connection"

	"github.com/winston-ci/prole/api"
	"github.com/winston-ci/prole/builder"
	"github.com/winston-ci/prole/checker"
	"github.com/winston-ci/prole/config"
	"github.com/winston-ci/prole/scheduler"
	"github.com/winston-ci/prole/sourcefetcher"
)

var listenAddr = flag.String(
	"listenAddr",
	"0.0.0.0:4637",
	"listening address",
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
	`{"git":"winston/git-resource","raw":"winston/raw-resource"}`,
	"map of resource type to its docker image",
)

func main() {
	flag.Parse()

	logger := log.New(os.Stdout, "", 0)

	wardenClient := WardenClient.New(&WardenConnection.Info{
		Network: *wardenNetwork,
		Addr:    *wardenAddr,
	})

	resourceTypesMap := map[string]string{}
	err := json.Unmarshal([]byte(*resourceTypes), &resourceTypesMap)
	if err != nil {
		log.Fatalln("failed to parse resource types:", err)
	}

	var resourceTypesConfig config.ResourceTypes
	for typ, image := range resourceTypesMap {
		resourceTypesConfig = append(resourceTypesConfig, config.ResourceType{
			Name:  typ,
			Image: image,
		})
	}

	sourceFetcher := sourcefetcher.NewSourceFetcher(resourceTypesConfig, wardenClient)
	checker := checker.NewChecker(resourceTypesConfig, wardenClient)

	builder := builder.NewBuilder(sourceFetcher, wardenClient)

	handler, err := api.New(scheduler.NewScheduler(builder), checker)
	if err != nil {
		log.Fatalln("failed to initialize handler:", err)
	}

	err = http.ListenAndServe(*listenAddr, handler)
	logger.Fatalln("listen error:", err)
}
