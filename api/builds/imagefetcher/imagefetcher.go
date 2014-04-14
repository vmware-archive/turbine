package imagefetcher

import (
	"os"

	"github.com/fsouza/go-dockerclient"
)

type DockerImageClient interface {
	PullImage(opts docker.PullImageOptions, auth docker.AuthConfiguration) error
	InspectImage(name string) (*docker.Image, error)
}

type ImageFetcher struct {
	dockerImageClient DockerImageClient
}

func NewImageFetcher(dockerImageClient DockerImageClient) *ImageFetcher {
	return &ImageFetcher{
		dockerImageClient: dockerImageClient,
	}
}

func (fetcher *ImageFetcher) Fetch(name string) (string, error) {
	err := fetcher.dockerImageClient.PullImage(
		docker.PullImageOptions{
			Repository:   name,
			OutputStream: os.Stderr,
		},
		docker.AuthConfiguration{},
	)
	if err != nil {
		return "", err
	}

	image, err := fetcher.dockerImageClient.InspectImage(name)
	if err != nil {
		return "", err
	}

	return image.ID, nil
}
