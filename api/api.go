package api

import (
	"log"

	"github.com/go-martini/martini"
)

func New(logger *log.Logger) *martini.ClassicMartini {
	m := martini.Classic()
	m.Map(logger)

	m.Get("/builds/:build_id", func() string {
		return "a build"
	})

	return m
}
