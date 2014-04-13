package api

import (
	"log"
	"net/http"
	"net/url"

	"github.com/rcrowley/go-tigertonic"
)

func New(logger *log.Logger) http.Handler {
	mux := tigertonic.NewTrieServeMux()

	getBuild := tigertonic.Logged(tigertonic.Marshaled(getBuild), nil)
	getBuild.Logger = logger

	mux.Handle(
		"GET",
		"/builds/{guid}",
		getBuild,
	)

	return mux
}

func getBuild(*url.URL, http.Header, interface{}) (int, http.Header, interface{}, error) {
	return http.StatusOK, nil, "ok", nil
}
