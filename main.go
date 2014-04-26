package main

import (
	"flag"
	"log"
	"os"

	"github.com/cloudfoundry-incubator/executor/log_streamer"
	"github.com/cloudfoundry-incubator/executor/log_streamer_factory"
	WardenClient "github.com/cloudfoundry-incubator/garden/client"
	WardenConnection "github.com/cloudfoundry-incubator/garden/client/connection"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/rcrowley/go-tigertonic"

	"github.com/winston-ci/prole/api"
	"github.com/winston-ci/prole/builder"
	"github.com/winston-ci/prole/scheduler"
	"github.com/winston-ci/prole/sourcefetcher"
)

var listenAddr = flag.String(
	"listen",
	"0.0.0.0:4637",
	"listening address",
)

var tmpdir = flag.String(
	"tmpdir",
	os.Getenv("TMPDIR"),
	"directory in which to store ephemeral data",
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

var loggregatorServer = flag.String(
	"loggregatorServer",
	"",
	"loggregator server to emit logs to",
)

var loggregatorSecret = flag.String(
	"loggregatorSecret",
	"",
	"secret for the loggregator server",
)

func main() {
	flag.Parse()

	logger := log.New(os.Stdout, "", 0)
	logger.Println("Booting...")

	wardenClient := WardenClient.New(&WardenConnection.Info{
		Network: *wardenNetwork,
		Addr:    *wardenAddr,
	})

	extractor := extractor.NewDetectable()
	sourceFetcher := sourcefetcher.NewSourceFetcher(*tmpdir, extractor)

	var logStreamerFactory log_streamer_factory.LogStreamerFactory
	if *loggregatorServer != "" {
		logStreamerFactory = log_streamer_factory.New(*loggregatorServer, *loggregatorSecret)
	} else {
		logStreamerFactory = func(models.LogConfig) log_streamer.LogStreamer {
			return log_streamer.NoopStreamer{}
		}
	}

	builder := builder.NewBuilder(sourceFetcher, wardenClient, logStreamerFactory)

	handler := api.New(logger, scheduler.NewScheduler(builder))

	server := tigertonic.NewServer(*listenAddr, handler)

	err := server.ListenAndServe()
	logger.Fatalln("listen error:", err)
}
