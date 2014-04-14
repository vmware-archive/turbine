package fakedocker

import (
	"sync"

	"github.com/fsouza/go-dockerclient"
)

type Client struct {
	pulledImages   []PulledImage
	PullImageError error

	InspectImageResult *docker.Image
	InspectImageError  error

	sync.RWMutex
}

type PulledImage struct {
	docker.PullImageOptions
	docker.AuthConfiguration
}

func NewClient() *Client {
	return &Client{}
}

func (client *Client) PullImage(opts docker.PullImageOptions, auth docker.AuthConfiguration) error {
	if client.PullImageError != nil {
		return client.PullImageError
	}

	client.Lock()

	client.pulledImages = append(client.pulledImages, PulledImage{
		PullImageOptions:  opts,
		AuthConfiguration: auth,
	})

	client.Unlock()

	return nil
}

func (client *Client) PulledImages() []PulledImage {
	client.RLock()

	pulledImages := make([]PulledImage, len(client.pulledImages))
	copy(pulledImages, client.pulledImages)

	client.RUnlock()

	return pulledImages
}

func (client *Client) InspectImage(name string) (*docker.Image, error) {
	if client.InspectImageError != nil {
		return nil, client.InspectImageError
	}

	return client.InspectImageResult, nil
}
