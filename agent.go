package main

import (
	"flag"
	"log"
	"os"

	"github.com/rcrowley/go-tigertonic"

	"github.com/room101-ci/agent/api"
)

var listenAddr = flag.String(
	"listen",
	"0.0.0.0:4637",
	"listening address",
)

func main() {
	logger := log.New(os.Stdout, "", 0)
	logger.Println("Booting...")

	server := tigertonic.NewServer(*listenAddr, api.New(logger))

	err := server.ListenAndServe()
	logger.Fatalln("listen error:", err)
}
