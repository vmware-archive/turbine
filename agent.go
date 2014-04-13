package main

import (
	"log"
	"os"

	"github.com/room101-ci/agent/api"
)

func main() {
	logger := log.New(os.Stdout, "", 0)
	logger.Println("Booting...")

	api := api.New(logger)
	api.Run()
}
