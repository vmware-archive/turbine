package sourcefetcher_test

import (
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cloudfoundry/gunk/command_runner/fake_command_runner"
	. "github.com/cloudfoundry/gunk/command_runner/fake_command_runner/matchers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	Extractor "github.com/pivotal-golang/archiver/extractor"
	"github.com/pivotal-golang/archiver/extractor/fake_extractor"

	archiver "github.com/pivotal-golang/archiver/extractor/test_helper"
	"github.com/winston-ci/prole/api/builds"
	. "github.com/winston-ci/prole/sourcefetcher"
)

var _ = Describe("SourceFetcher", func() {
	var tmpdir string
	var extractor Extractor.Extractor
	var commandRunner *fake_command_runner.FakeCommandRunner
	var sourceFetcher *SourceFetcher

	var server *ghttp.Server

	BeforeEach(func() {
		var err error

		tmpdir, err = ioutil.TempDir("", "agent-source-fetcher")
		Ω(err).ShouldNot(HaveOccurred())

		server = ghttp.NewServer()

		extractor = Extractor.NewDetectable()

		commandRunner = fake_command_runner.New()
	})

	JustBeforeEach(func() {
		sourceFetcher = NewSourceFetcher(tmpdir, extractor, commandRunner)
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

	Describe("fetching a git source", func() {
		var source builds.BuildSource

		BeforeEach(func() {
			source = builds.BuildSource{
				Type:   "git",
				URI:    "git://example.com/some/repo.git",
				Branch: "some-branch",
			}
		})

		It("clones it shallowly to a tmpdir, and checks out the ref", func() {
			fetched, err := sourceFetcher.Fetch(source)
			Ω(err).ShouldNot(HaveOccurred())

			Ω(commandRunner).Should(HaveExecutedSerially(
				fake_command_runner.CommandSpec{
					Path: "git",
					Args: []string{
						"clone",
						"--depth", "10",
						"--branch", source.Branch,
						source.URI,
						fetched,
					},
				},
				fake_command_runner.CommandSpec{
					Path: "git",
					Args: []string{"checkout", source.Ref},
					Dir:  fetched,
				},
			))
		})

		Context("when cloning fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				commandRunner.WhenRunning(
					fake_command_runner.CommandSpec{
						Path: "git",
					}, func(cmd *exec.Cmd) error {
						if cmd.Args[0] == "clone" {
							return disaster
						} else {
							return nil
						}
					},
				)
			})

			It("returns the error", func() {
				_, err := sourceFetcher.Fetch(source)
				Ω(err).Should(Equal(disaster))
			})
		})

		Context("when checking out fails", func() {
			disaster := errors.New("oh no!")

			BeforeEach(func() {
				commandRunner.WhenRunning(
					fake_command_runner.CommandSpec{
						Path: "git",
					}, func(cmd *exec.Cmd) error {
						if cmd.Args[0] == "checkout" {
							return disaster
						} else {
							return nil
						}
					},
				)
			})

			It("returns the error", func() {
				_, err := sourceFetcher.Fetch(source)
				Ω(err).Should(Equal(disaster))
			})
		})
	})
})
