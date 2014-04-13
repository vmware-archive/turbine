package api_test

import (
	"log"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestApi(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "API Suite")
}

func NullLogger() *log.Logger {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		panic("Could not make a null logger")
	}
	return log.New(devNull, "", 0)
}
