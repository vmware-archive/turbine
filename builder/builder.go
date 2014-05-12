package builder

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/fraenkel/candiedyaml"
	"github.com/gorilla/websocket"
	"github.com/pivotal-golang/archiver/compressor"

	"github.com/winston-ci/prole/api/builds"
)

type Builder interface {
	Build(builds.Build) (bool, error)
}

type SourceFetcher interface {
	Fetch(source builds.BuildSource) (directory string, err error)
}

type ImageFetcher interface {
	Fetch(name string) (id string, err error)
}

type ConfigFile struct {
	Image  string   `yaml:"image"`
	Env    []string `yaml:"env"`
	Script string   `yaml:"script"`
}

type builder struct {
	tmpdir        string
	sourceFetcher SourceFetcher
	wardenClient  warden.Client
}

func NewBuilder(
	tmpdir string,
	sourceFetcher SourceFetcher,
	wardenClient warden.Client,
) Builder {
	return &builder{
		tmpdir:        tmpdir,
		sourceFetcher: sourceFetcher,
		wardenClient:  wardenClient,
	}
}

func (builder *builder) Build(build builds.Build) (bool, error) {
	var logsEndpoint *websocket.Conn

	if build.LogsURL != "" {
		conn, resp, err := websocket.DefaultDialer.Dial(build.LogsURL, nil)
		if err != nil {
			return false, err
		}

		defer conn.Close()
		defer resp.Body.Close()

		logsEndpoint = conn
	}

	buildSrc, err := ioutil.TempDir(builder.tmpdir, "build-src")
	if err != nil {
		return false, err
	}

	defer os.RemoveAll(buildSrc)

	for _, source := range build.Sources {
		fetchedSource, err := builder.sourceFetcher.Fetch(source)
		if err != nil {
			return false, err
		}

		tmpdest := filepath.Join(buildSrc, source.Path)

		err = os.MkdirAll(filepath.Dir(tmpdest), 0755)
		if err != nil {
			return false, err
		}

		err = os.Rename(fetchedSource, tmpdest)
		if err != nil {
			return false, err
		}
	}

	if build.ConfigPath != "" {
		configFile, err := os.Open(filepath.Join(buildSrc, build.ConfigPath))
		if err != nil {
			return false, err
		}

		defer configFile.Close()

		var config ConfigFile
		err = candiedyaml.NewDecoder(configFile).Decode(&config)
		if err != nil {
			return false, err
		}

		build.Image = config.Image
		build.Script = config.Script
		build.Env = [][2]string{}

		for _, env := range config.Env {
			segs := strings.SplitN(env, "=", 2)
			if len(segs) != 2 {
				return false, fmt.Errorf("invalid env string: %q", env)
			}

			build.Env = append(build.Env, [2]string{segs[0], segs[1]})
		}
	}

	if logsEndpoint != nil {
		logsEndpoint.WriteMessage(
			websocket.TextMessage,
			[]byte("creating container from "+build.Image+"...\n"),
		)
	}

	container, err := builder.wardenClient.Create(warden.ContainerSpec{
		RootFSPath: "image:" + build.Image,
	})
	if err != nil {
		return false, err
	}

	streamIn, err := container.StreamIn("/tmp/build/src")
	if err != nil {
		return false, err
	}

	err = compressor.WriteTar(buildSrc+"/", streamIn)
	if err != nil {
		return false, err
	}

	err = streamIn.Close()
	if err != nil {
		return false, err
	}

	if logsEndpoint != nil {
		logsEndpoint.WriteMessage(websocket.TextMessage, []byte("starting...\n"))
	}

	env := make([]warden.EnvironmentVariable, len(build.Env))
	for i, e := range build.Env {
		env[i] = warden.EnvironmentVariable{
			Key:   e[0],
			Value: e[1],
		}
	}

	_, stream, err := container.Run(warden.ProcessSpec{
		Privileged: build.Privileged,

		Script: "cd /tmp/build/src\n" + build.Script,

		EnvironmentVariables: env,
	})
	if err != nil {
		return false, err
	}

	succeeded := false

	for chunk := range stream {
		if chunk.ExitStatus != nil {
			succeeded = *chunk.ExitStatus == 0

			if logsEndpoint != nil {
				logsEndpoint.WriteControl(websocket.CloseMessage, nil, time.Time{})
			}

			break
		}

		if logsEndpoint != nil {
			logsEndpoint.WriteMessage(websocket.BinaryMessage, chunk.Data)
		}
	}

	return succeeded, nil
}
