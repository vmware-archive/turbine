package builder_test

import (
	"errors"

	"github.com/cloudfoundry-incubator/gordon"
	"github.com/cloudfoundry-incubator/gordon/fake_gordon"
	"github.com/cloudfoundry-incubator/gordon/warden"
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

	primedStream := func(payloads ...*warden.ProcessPayload) <-chan *warden.ProcessPayload {
		stream := make(chan *warden.ProcessPayload, len(payloads))

		for _, payload := range payloads {
			stream <- payload
		}

		close(stream)

		return stream
	}

	BeforeEach(func() {
		sourceFetcher = fakesourcefetcher.New()
		imageFetcher = fakeimagefetcher.New()
		wardenClient = fake_gordon.New()
		builder = NewBuilder(sourceFetcher, imageFetcher, wardenClient)

		build = &builds.Build{
			Image: "some-image-name",

			Script: "./bin/test",

			Source: builds.BuildSource{
				Type: "raw",
				URI:  "http://example.com/foo.tar.gz",
			},
		}

		exitStatus := uint32(0)

		successfulStream := primedStream(&warden.ProcessPayload{
			ExitStatus: &exitStatus,
		})

		wardenClient.SetRunReturnValues(42, successfulStream, nil)
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

	It("fetches the build source and copies it in to the container", func() {
		sourceFetcher.FetchResult = "/path/on/disk"

		_, err := builder.Build(build)
		Ω(err).ShouldNot(HaveOccurred())

		Ω(sourceFetcher.Fetched()).Should(ContainElement(build.Source))

		handle := wardenClient.Created()[0].Handle

		Ω(wardenClient.ThingsCopiedIn()).Should(ContainElement(&fake_gordon.CopiedIn{
			Handle: handle,
			Src:    "/path/on/disk/",
			Dst:    "./",
		}))
	})

	It("runs the build's script in the container", func() {
		_, err := builder.Build(build)
		Ω(err).ShouldNot(HaveOccurred())

		handle := wardenClient.Created()[0].Handle

		Ω(wardenClient.ScriptsThatRan()).Should(ContainElement(&fake_gordon.RunningScript{
			Handle: handle,
			Script: "./bin/test",
		}))
	})

	Context("when running the build's script fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			wardenClient.WhenRunning(
				"",
				"./bin/test",
				gordon.ResourceLimits{},
				func() (uint32, <-chan *warden.ProcessPayload, error) {
					return 0, nil, disaster
				},
			)
		})

		It("returns true", func() {
			succeeded, err := builder.Build(build)
			Ω(err).Should(Equal(disaster))
			Ω(succeeded).Should(BeFalse())
		})
	})

	Context("when the build's script exits 0", func() {
		BeforeEach(func() {
			wardenClient.WhenRunning(
				"",
				"./bin/test",
				gordon.ResourceLimits{},
				func() (uint32, <-chan *warden.ProcessPayload, error) {
					exitStatus := uint32(0)

					return 42, primedStream(&warden.ProcessPayload{
						ExitStatus: &exitStatus,
					}), nil
				},
			)
		})

		It("returns true", func() {
			succeeded, err := builder.Build(build)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(succeeded).Should(BeTrue())
		})
	})

	Context("when the build's script exits nonzero", func() {
		BeforeEach(func() {
			wardenClient.WhenRunning(
				"",
				"./bin/test",
				gordon.ResourceLimits{},
				func() (uint32, <-chan *warden.ProcessPayload, error) {
					exitStatus := uint32(42)

					return 42, primedStream(&warden.ProcessPayload{
						ExitStatus: &exitStatus,
					}), nil
				},
			)
		})

		It("returns true", func() {
			succeeded, err := builder.Build(build)
			Ω(err).ShouldNot(HaveOccurred())
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

	Context("when copying the source in to the container fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			wardenClient.SetCopyInErr(disaster)
		})

		It("returns the error", func() {
			succeeded, err := builder.Build(build)
			Ω(err).Should(Equal(disaster))
			Ω(succeeded).Should(BeFalse())
		})
	})
})
