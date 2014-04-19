package imagefetcher_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestImagefetcher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Imagefetcher Suite")
}
