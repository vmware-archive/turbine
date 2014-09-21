package inputs_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestInputs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Inputs Suite")
}
