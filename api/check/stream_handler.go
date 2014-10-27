package check

import (
	"io"
	"io/ioutil"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/pivotal-golang/lager"

	"github.com/concourse/turbine"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool {
		return true
	},
}

func (handler *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		handler.logger.Error("upgrade-failed", err)
		return
	}

	defer conn.Close()

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-handler.drain:
			conn.Close()
		case <-done:
		}
	}()

	var input turbine.Input
	err = conn.ReadJSON(&input)
	if err != nil {
		handler.logger.Error("malformed-request", err)
		return
	}

	log := handler.logger.Session("streaming-check", lager.Data{
		"input": input,
	})

	resource, err := handler.tracker.Init(input.Type, ioutil.Discard, handler.drain)
	if err != nil {
		log.Error("failed-to-init", err)
		return
	}

	defer handler.tracker.Release(resource)

	for {
		log.Info("checking")

		versions, err := resource.Check(input)
		if err != nil {
			log.Error("failed-to-check", err)
			return
		}

		err = conn.WriteJSON(versions)
		if err != nil {
			log.Error("failed-to-encode", err)
			break
		}

		err = conn.ReadJSON(&input)
		if err == io.EOF {
			break
		}

		if err != nil {
			select {
			case <-handler.drain:
			default:
				log.Error("malformed-request", err)
			}

			return
		}
	}
}
