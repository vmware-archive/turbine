package sourcefetcher_test

import (
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	Extractor "github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/archiver/extractor/fake_extractor"

	"github.com/winston-ci/prole/api/builds"
	. "github.com/winston-ci/prole/sourcefetcher"
	"github.com/winston-ci/prole/testhelpers/archiver"
)

var _ = Describe("SourceFetcher", func() {
	var tmpdir string
	var extractor Extractor.Extractor
	var sourceFetcher *SourceFetcher

	var server *ghttp.Server

	BeforeEach(func() {
		var err error

		tmpdir, err = ioutil.TempDir("", "agent-source-fetcher")
		Ω(err).ShouldNot(HaveOccurred())

		server = ghttp.NewServer()

		extractor = Extractor.NewDetectable()
	})

	JustBeforeEach(func() {
		sourceFetcher = NewSourceFetcher(tmpdir, extractor)
	})

	Describe("fetching a raw source", func() {
		Context("when the url is a .tar.gz", func() {
			var archivePath string
			var source builds.BuildSource

			BeforeEach(func() {
				tmpfile, err := ioutil.TempFile("", "agent-source-fetcher-tgz")
				Ω(err).ShouldNot(HaveOccurred())

				archivePath := tmpfile.Name()

				os.Remove(archivePath)

				archiver.CreateTarGZArchive(archivePath, []archiver.ArchiveFile{
					{
						Name: "some-file",
						Body: "some-file-contents",
					},
				})

				source = builds.BuildSource{
					Type: "raw",
					URI:  server.URL() + "/foo.tar.gz",
				}

				server.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/foo.tar.gz"),
						func(w http.ResponseWriter, _ *http.Request) {
							w.WriteHeader(http.StatusOK)

							archive, err := os.Open(archivePath)
							Ω(err).ShouldNot(HaveOccurred())

							_, err = io.Copy(w, archive)
							Ω(err).ShouldNot(HaveOccurred())
						},
					),
				)
			})

			AfterEach(func() {
				os.Remove(archivePath)
			})

			It("downloads it into the temporary directory", func() {
				fetched, err := sourceFetcher.Fetch(source)
				Ω(err).ShouldNot(HaveOccurred())

				body, err := ioutil.ReadFile(filepath.Join(fetched, "some-file"))
				Ω(err).ShouldNot(HaveOccurred())

				Ω(string(body)).Should(Equal("some-file-contents"))
			})

			Context("when downloading fails", func() {
				BeforeEach(func() {
					server.Close()
				})

				It("returns an error", func() {
					_, err := sourceFetcher.Fetch(source)
					Ω(err).Should(HaveOccurred())
				})
			})

			Context("when extracting fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					fakeExtractor := &fake_extractor.FakeExtractor{}
					fakeExtractor.SetExtractOutput(disaster)

					extractor = fakeExtractor
				})

				It("returns the error", func() {
					_, err := sourceFetcher.Fetch(source)
					Ω(err).Should(Equal(disaster))
				})
			})
		})
	})
})
