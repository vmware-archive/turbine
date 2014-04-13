package builds

import (
	"net/http"
	"net/url"

	"github.com/rcrowley/go-tigertonic"
)

type Handler struct {
}

func NewHandler() *Handler {
	return &Handler{}
}

func (handler *Handler) GetHandler() http.Handler {
	return tigertonic.Marshaled(handler.get)
}

func (handler *Handler) get(url *url.URL, header http.Header, _ interface{}) (int, http.Header, *Build, error) {
	return http.StatusOK, nil, &Build{}, nil
}
