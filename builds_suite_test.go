package turbine_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestTurbine(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Turbine Suite")
}
