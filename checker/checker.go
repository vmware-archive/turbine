package checker

import (
	"errors"

	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/config"
	"github.com/winston-ci/prole/scriptrunner"
)

type Checker interface {
	Check(builds.Input) ([]builds.Version, error)
}

var ErrUnknownSourceType = errors.New("unknown source type")

type checker struct {
	resourceTypes config.ResourceTypes
	wardenClient  warden.Client
}

type versionAndSource struct {
	Version builds.Version `json:"version"`
	Source  builds.Source  `json:"source"`
}

func NewChecker(
	resourceTypes config.ResourceTypes,
	wardenClient warden.Client,
) Checker {
	return &checker{
		resourceTypes: resourceTypes,
		wardenClient:  wardenClient,
	}
}

func (checker *checker) Check(input builds.Input) ([]builds.Version, error) {
	resourceType, found := checker.resourceTypes.Lookup(input.Type)
	if !found {
		return nil, ErrUnknownSourceType
	}

	container, err := checker.wardenClient.Create(warden.ContainerSpec{
		RootFSPath: "docker:///" + resourceType.Image,
	})
	if err != nil {
		return nil, err
	}

	defer checker.wardenClient.Destroy(container.Handle())

	var versions []builds.Version

	err = scriptrunner.Run(
		container,
		"/tmp/resource/check",
		nil,
		versionAndSource{input.Version, input.Source},
		&versions,
	)
	if err != nil {
		return nil, err
	}

	return versions, nil
}
