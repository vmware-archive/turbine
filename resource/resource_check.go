package resource

import "github.com/winston-ci/prole/api/builds"

type versionAndSource struct {
	Version builds.Version `json:"version"`
	Source  builds.Source  `json:"source"`
}

func (resource *resource) Check(input builds.Input) ([]builds.Version, error) {
	var versions []builds.Version

	err := resource.runScript(
		"/tmp/resource/check",
		versionAndSource{input.Version, input.Source},
		&versions,
	)
	if err != nil {
		return nil, err
	}

	return versions, nil
}
