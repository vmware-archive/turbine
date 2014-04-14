package sourcefetcher_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestSourcefetcher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sourcefetcher Suite")
}
