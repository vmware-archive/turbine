package resource

import "github.com/concourse/turbine"

type versionAndSource struct {
	Version turbine.Version `json:"version"`
	Source  turbine.Source  `json:"source"`
}

func (resource *resource) Check(input turbine.Input) ([]turbine.Version, error) {
	var versions []turbine.Version

	err := resource.runScript(
		"/opt/resource/check",
		nil,
		versionAndSource{input.Version, input.Source},
		&versions,
	)
	if err != nil {
		return nil, err
	}

	return versions, nil
}
