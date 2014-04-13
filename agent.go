package main

import (
	"flag"
	"log"
	"os"

	"github.com/rcrowley/go-tigertonic"

	"github.com/room101-ci/agent/api"
	"github.com/room101-ci/agent/api/builds/scheduler"
)

var listenAddr = flag.String(
	"listen",
	"0.0.0.0:4637",
	"listening address",
)

func main() {
	logger := log.New(os.Stdout, "", 0)
	logger.Println("Booting...")

	handler := api.New(logger, scheduler.NewScheduler())
	server := tigertonic.NewServer(*listenAddr, handler)

	err := server.ListenAndServe()
	logger.Fatalln("listen error:", err)
}
