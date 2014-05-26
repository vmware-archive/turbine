package checker

import (
	"errors"

	"github.com/cloudfoundry-incubator/garden/warden"
	"github.com/winston-ci/prole/api/builds"
	"github.com/winston-ci/prole/config"
	"github.com/winston-ci/prole/scriptrunner"
)

type Checker interface {
	Check(builds.Input) ([]builds.Source, error)
}

var ErrUnknownSourceType = errors.New("unknown source type")

type checker struct {
	resourceTypes config.ResourceTypes
	wardenClient  warden.Client
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

func (checker *checker) Check(input builds.Input) ([]builds.Source, error) {
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

	var sources []builds.Source

	err = scriptrunner.Run(
		container,
		"/tmp/resource/check",
		nil,
		input.Source,
		&sources,
	)
	if err != nil {
		return nil, err
	}

	return sources, nil
}
