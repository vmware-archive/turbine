package resource_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/winston-ci/prole/resource"
)

var _ = Describe("ConfigFile", func() {
	var cf ConfigFile

	Describe("AsBuildConfig", func() {
		BeforeEach(func() {
			cf = ConfigFile{
				Image: "some-image-name",

				Env: []string{
					"A=B",
					"C=D",
				},

				Script: "./some/script",
			}
		})

		It("works", func() {
			config, err := cf.AsBuildConfig()
			Ω(err).ShouldNot(HaveOccurred())

			Ω(config.Image).Should(Equal(cf.Image))
			Ω(config.Script).Should(Equal(cf.Script))
			Ω(config.Env).Should(Equal([][2]string{
				{"A", "B"},
				{"C", "D"},
			}))
		})

		Context("when the env is malformed", func() {
			BeforeEach(func() {
				cf.Env[0] = "lol"
			})

			It("does not work", func() {
				_, err := cf.AsBuildConfig()
				Ω(err).Should(HaveOccurred())
			})
		})
	})
})
