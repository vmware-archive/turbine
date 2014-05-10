package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	Path   string   `yaml:"path"`
	Env    []string `yaml:"env"`
	Script string   `yaml:"script"`
}

type builder struct {
	sourceFetcher SourceFetcher
	wardenClient  warden.Client
}

func NewBuilder(
	sourceFetcher SourceFetcher,
	wardenClient warden.Client,
) Builder {
	return &builder{
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

	fetchedSource, err := builder.sourceFetcher.Fetch(build.Source)
	if err != nil {
		return false, err
	}

	if build.ConfigPath != "" {
		configFile, err := os.Open(filepath.Join(fetchedSource, build.ConfigPath))
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
		build.Source.Path = config.Path
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

	streamIn, err := container.StreamIn(build.Source.Path)
	if err != nil {
		return false, err
	}

	err = compressor.WriteTar(fetchedSource+"/", streamIn)
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
		Script:               build.Script,
		EnvironmentVariables: env,
	})
	if err != nil {
		return false, err
	}

	succeeded := false

	for chunk := range stream {
		if chunk.ExitStatus != nil {
			succeeded = *chunk.ExitStatus == 0
			break
		}

		if logsEndpoint != nil {
			logsEndpoint.WriteMessage(websocket.BinaryMessage, chunk.Data)
		}
	}

	return succeeded, nil
}
