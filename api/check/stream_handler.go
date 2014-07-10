package check

import (
	"encoding/json"
	"io"
	"io/ioutil"

	"code.google.com/p/go.net/websocket"
	"github.com/pivotal-golang/lager"

	"github.com/concourse/turbine/api/builds"
)

func (handler *Handler) Stream(conn *websocket.Conn) {
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-handler.drain:
			conn.Close()
		case <-done:
		}
	}()

	var input builds.Input
	err := json.NewDecoder(conn).Decode(&input)
	if err != nil {
		handler.logger.Error("malformed-request", err)
		return
	}

	log := handler.logger.Session("streaming-check", lager.Data{
		"input": input,
	})

	resource, err := handler.tracker.Init(input.Type, ioutil.Discard, nil)
	if err != nil {
		log.Error("failed-to-init", err)
		return
	}

	defer handler.tracker.Release(resource)

	encoder := json.NewEncoder(conn)

	for {
		log.Info("checking")

		versions, err := resource.Check(input)
		if err != nil {
			log.Error("failed-to-check", err)
			return
		}

		err = encoder.Encode(versions)
		if err != nil {
			log.Error("failed-to-encode", err)
			break
		}

		err = json.NewDecoder(conn).Decode(&input)
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
