package builder_test

import (
	"errors"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/room101-ci/agent/api/builds"
	. "github.com/room101-ci/agent/api/builds/builder"
	"github.com/room101-ci/agent/api/builds/fetcher/fakefetcher"
)

var _ = Describe("Builder", func() {
	var fetcher *fakefetcher.Fetcher
	var builder *Builder

	var build *builds.Build

	BeforeEach(func() {
		fetcher = fakefetcher.New()
		builder = NewBuilder(fetcher)

		build = &builds.Build{
			Source: builds.BuildSource{
				Type: "raw",
				URI:  "http://example.com/foo.tar.gz",
			},
		}
	})

	It("fetches the build source", func() {
		_, err := builder.Build(build)
		Ω(err).ShouldNot(HaveOccurred())

		Ω(fetcher.Fetched()).Should(ContainElement(build.Source))
	})

	Context("when fetching the source fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			fetcher.FetchError = disaster
		})

		It("returns the error", func() {
			succeeded, err := builder.Build(build)
			Ω(err).Should(Equal(disaster))
			Ω(succeeded).Should(BeFalse())
		})
	})
})
