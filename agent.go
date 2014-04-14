package main

import (
	"flag"
	"log"
	"os"

	"github.com/cloudfoundry-incubator/gordon"
	"github.com/fsouza/go-dockerclient"
	"github.com/pivotal-golang/archiver/extractor"
	"github.com/rcrowley/go-tigertonic"

	"github.com/room101-ci/agent/api"
	"github.com/room101-ci/agent/api/builds/builder"
	"github.com/room101-ci/agent/api/builds/imagefetcher"
	"github.com/room101-ci/agent/api/builds/scheduler"
	"github.com/room101-ci/agent/api/builds/sourcefetcher"
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

var dockerEndpoint = flag.String(
	"dockerEndpoint",
	os.Getenv("DOCKER_HOST"),
	"docker remote API endpoint",
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
	logger.Println("Booting...")

	dockerClient, err := docker.NewClient(*dockerEndpoint)
	if err != nil {
		logger.Fatalln("could not initialize docker client:", err)
	}

	wardenClient := gordon.NewClient(&gordon.ConnectionInfo{
		Network: *wardenNetwork,
		Addr:    *wardenAddr,
	})

	extractor := extractor.NewDetectable()
	sourceFetcher := sourcefetcher.NewSourceFetcher(*tmpdir, extractor)
	imageFetcher := imagefetcher.NewImageFetcher(dockerClient)

	builder := builder.NewBuilder(sourceFetcher, imageFetcher, wardenClient)

	handler := api.New(logger, scheduler.NewScheduler(builder))

	server := tigertonic.NewServer(*listenAddr, handler)

	err = server.ListenAndServe()
	logger.Fatalln("listen error:", err)
}
