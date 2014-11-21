package resource_test

import (
	"errors"

	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/cloudfoundry-incubator/garden/client/fake_api_client"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/concourse/turbine/config"
	. "github.com/concourse/turbine/resource"
)

var _ = Describe("Tracker", func() {
	var (
		resourceTypes config.ResourceTypes
		gardenClient  *fake_api_client.FakeClient

		tracker Tracker
	)

	BeforeEach(func() {
		resourceTypes = config.ResourceTypes{
			{Name: "type1", Image: "image1"},
			{Name: "type2", Image: "image2"},
		}

		gardenClient = fake_api_client.New()

		gardenClient.Connection.CreateReturns("some-handle", nil)

		tracker = NewTracker(resourceTypes, gardenClient)
	})

	Describe("Init", func() {
		var (
			initType string

			initResource Resource
			initErr      error
		)

		BeforeEach(func() {
			initType = "type1"
		})

		JustBeforeEach(func() {
			initResource, initErr = tracker.Init(initType, nil, nil)
		})

		It("does not error and returns a resource", func() {
			Ω(initErr).ShouldNot(HaveOccurred())
			Ω(initResource).ShouldNot(BeNil())
		})

		It("creates a privileged container with the resource type's image", func() {
			Ω(gardenClient.Connection.CreateArgsForCall(0)).Should(Equal(garden_api.ContainerSpec{
				RootFSPath: "image1",
				Privileged: true,
			}))
		})

		Context("when creating the container fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				gardenClient.Connection.CreateReturns("", disaster)
			})

			It("returns the error and no resource", func() {
				Ω(initErr).Should(Equal(disaster))
				Ω(initResource).Should(BeNil())
			})
		})

		Context("with an unknown resource type", func() {
			BeforeEach(func() {
				initType = "bogus-type"
			})

			It("returns ErrUnknownResourceType", func() {
				Ω(initErr).Should(Equal(ErrUnknownResourceType))
			})
		})
	})

	Describe("Release", func() {
		var (
			releaseResource Resource

			releaseErr error
		)

		BeforeEach(func() {
			var err error

			releaseResource, err = tracker.Init("type1", nil, nil)
			Ω(err).ShouldNot(HaveOccurred())
		})

		JustBeforeEach(func() {
			releaseErr = tracker.Release(releaseResource)
		})

		It("does not error", func() {
			Ω(releaseErr).ShouldNot(HaveOccurred())
		})

		It("destroys the container associated with the resource", func() {
			Ω(gardenClient.Connection.DestroyCallCount()).Should(Equal(1))
			Ω(gardenClient.Connection.DestroyArgsForCall(0)).Should(Equal("some-handle"))
		})

		Context("when destroying the container fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				gardenClient.Connection.DestroyReturns(disaster)
			})

			It("returns the error", func() {
				Ω(releaseErr).Should(Equal(disaster))
			})
		})

		Context("with an unknown resource", func() {
			BeforeEach(func() {
				releaseResource = nil
			})

			It("returns no error", func() {
				Ω(releaseErr).ShouldNot(HaveOccurred())
			})

			It("destroys no containers", func() {
				Ω(gardenClient.Connection.DestroyCallCount()).Should(BeZero())
			})
		})
	})
})
