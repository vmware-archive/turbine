package resource

import (
	"errors"
	"io"
	"sync"

	garden_api "github.com/cloudfoundry-incubator/garden/api"
	"github.com/concourse/turbine/config"
)

type Tracker interface {
	Init(typ string, logs io.Writer, abort <-chan struct{}) (Resource, error)
	Release(Resource) error
}

type tracker struct {
	resourceTypes config.ResourceTypes
	gardenClient  garden_api.Client

	containers  map[Resource]garden_api.Container
	containersL *sync.Mutex
}

var ErrUnknownResourceType = errors.New("unknown resource type")

func NewTracker(resourceTypes config.ResourceTypes, gardenClient garden_api.Client) Tracker {
	return &tracker{
		resourceTypes: resourceTypes,
		gardenClient:  gardenClient,

		containers:  make(map[Resource]garden_api.Container),
		containersL: new(sync.Mutex),
	}
}

func (tracker *tracker) Init(typ string, logs io.Writer, abort <-chan struct{}) (Resource, error) {
	resourceType, found := tracker.resourceTypes.Lookup(typ)
	if !found {
		return nil, ErrUnknownResourceType
	}

	container, err := tracker.gardenClient.Create(garden_api.ContainerSpec{
		RootFSPath: resourceType.Image,
	})
	if err != nil {
		return nil, err
	}

	resource := NewResource(container, logs, abort)

	tracker.containersL.Lock()
	tracker.containers[resource] = container
	tracker.containersL.Unlock()

	return resource, nil
}

func (tracker *tracker) Release(resource Resource) error {
	tracker.containersL.Lock()
	container, found := tracker.containers[resource]
	delete(tracker.containers, resource)
	tracker.containersL.Unlock()

	if !found {
		return nil
	}

	return tracker.gardenClient.Destroy(container.Handle())
}
