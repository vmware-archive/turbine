package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	WardenClient "github.com/cloudfoundry-incubator/garden/client"
	WardenConnection "github.com/cloudfoundry-incubator/garden/client/connection"

	"github.com/winston-ci/prole/api"
	"github.com/winston-ci/prole/builder"
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

func main() {
	flag.Parse()

	logger := log.New(os.Stdout, "", 0)

	wardenClient := WardenClient.New(&WardenConnection.Info{
		Network: *wardenNetwork,
		Addr:    *wardenAddr,
	})

	sourceFetcher := sourcefetcher.NewSourceFetcher(nil, wardenClient)

	builder := builder.NewBuilder(sourceFetcher, wardenClient)

	handler, err := api.New(scheduler.NewScheduler(builder))
	if err != nil {
		log.Fatalln("failed to initialize handler:", err)
	}

	err = http.ListenAndServe(*listenAddr, handler)
	logger.Fatalln("listen error:", err)
}
