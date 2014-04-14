package builder_test

import (
	"errors"

	"github.com/cloudfoundry-incubator/gordon/fake_gordon"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/room101-ci/agent/api/builds"
	. "github.com/room101-ci/agent/api/builds/builder"
	"github.com/room101-ci/agent/api/builds/imagefetcher/fakeimagefetcher"
	"github.com/room101-ci/agent/api/builds/sourcefetcher/fakesourcefetcher"
)

var _ = Describe("Builder", func() {
	var sourceFetcher *fakesourcefetcher.Fetcher
	var imageFetcher *fakeimagefetcher.Fetcher
	var wardenClient *fake_gordon.FakeGordon
	var builder *Builder

	var build *builds.Build

	BeforeEach(func() {
		sourceFetcher = fakesourcefetcher.New()
		imageFetcher = fakeimagefetcher.New()
		wardenClient = fake_gordon.New()
		builder = NewBuilder(sourceFetcher, imageFetcher, wardenClient)

		build = &builds.Build{
			Image: "some-image-name",

			Source: builds.BuildSource{
				Type: "raw",
				URI:  "http://example.com/foo.tar.gz",
			},
		}
	})

	It("fetches the build image and uses it for the container rootfs", func() {
		imageFetcher.FetchResult = "some-image-id"

		_, err := builder.Build(build)
		Ω(err).ShouldNot(HaveOccurred())

		Ω(imageFetcher.Fetched()).Should(ContainElement("some-image-name"))

		createdContainers := wardenClient.Created()
		Ω(createdContainers).Should(HaveLen(1))
		Ω(createdContainers[0].RootFSPath).Should(Equal("image:some-image-id"))
	})

	It("fetches the build source", func() {
		_, err := builder.Build(build)
		Ω(err).ShouldNot(HaveOccurred())

		Ω(sourceFetcher.Fetched()).Should(ContainElement(build.Source))
	})

	Context("when creating the container fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			wardenClient.CreateError = disaster
		})

		It("returns the error", func() {
			succeeded, err := builder.Build(build)
			Ω(err).Should(Equal(disaster))
			Ω(succeeded).Should(BeFalse())
		})
	})

	Context("when fetching the image fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			imageFetcher.FetchError = disaster
		})

		It("returns the error", func() {
			succeeded, err := builder.Build(build)
			Ω(err).Should(Equal(disaster))
			Ω(succeeded).Should(BeFalse())
		})
	})

	Context("when fetching the source fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			sourceFetcher.FetchError = disaster
		})

		It("returns the error", func() {
			succeeded, err := builder.Build(build)
			Ω(err).Should(Equal(disaster))
			Ω(succeeded).Should(BeFalse())
		})
	})
})
