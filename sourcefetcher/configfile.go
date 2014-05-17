package sourcefetcher

import (
	"fmt"
	"strings"

	"github.com/winston-ci/prole/api/builds"
)

type ConfigFile struct {
	Image  string   `yaml:"image"`
	Env    []string `yaml:"env"`
	Script string   `yaml:"script"`
}

func (cf ConfigFile) AsBuildConfig() (builds.Config, error) {
	buildConfig := builds.Config{
		Image:  cf.Image,
		Script: cf.Script,
		Env:    [][2]string{},
	}

	for _, env := range cf.Env {
		segs := strings.SplitN(env, "=", 2)
		if len(segs) != 2 {
			return buildConfig, fmt.Errorf("invalid env string: %q", env)
		}

		buildConfig.Env = append(buildConfig.Env, [2]string{segs[0], segs[1]})
	}

	return buildConfig, nil
}
