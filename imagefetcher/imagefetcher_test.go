package imagefetcher_test

import (
	"errors"
	"os"

	"github.com/fsouza/go-dockerclient"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/winston-ci/prole/imagefetcher"

	"github.com/winston-ci/prole/testhelpers/fakedocker"
)

var _ = Describe("Imagefetcher", func() {
	var dockerImageClient *fakedocker.Client
	var fetcher *ImageFetcher

	BeforeEach(func() {
		dockerImageClient = fakedocker.NewClient()
		fetcher = NewImageFetcher(dockerImageClient)
	})

	It("pulls the image and returns its id", func() {
		dockerImageClient.InspectImageResult = &docker.Image{
			ID: "some-id",
		}

		id, err := fetcher.Fetch("some-name")
		Ω(err).ShouldNot(HaveOccurred())

		Ω(dockerImageClient.PulledImages()).Should(ContainElement(fakedocker.PulledImage{
			PullImageOptions: docker.PullImageOptions{
				Repository:   "some-name",
				OutputStream: os.Stderr,
			},
		}))

		Ω(id).Should(Equal("some-id"))
	})

	Context("when pulling the image fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			dockerImageClient.PullImageError = disaster
		})

		It("returns the error", func() {
			_, err := fetcher.Fetch("some-name")
			Ω(err).Should(Equal(disaster))
		})
	})

	Context("when inspecting the image fails", func() {
		disaster := errors.New("oh no!")

		BeforeEach(func() {
			dockerImageClient.InspectImageError = disaster
		})

		It("returns the error", func() {
			_, err := fetcher.Fetch("some-name")
			Ω(err).Should(Equal(disaster))
		})
	})
})
