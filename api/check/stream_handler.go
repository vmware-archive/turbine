package check

import (
	"encoding/json"
	"io"
	"log"

	"code.google.com/p/go.net/websocket"

	"github.com/winston-ci/prole/api/builds"
)

func (handler *Handler) Stream(conn *websocket.Conn) {
	var input builds.Input
	err := json.NewDecoder(conn).Decode(&input)
	if err != nil {
		log.Println("malformed request:", err)
		return
	}

	log.Println("streaming checks for", input)

	resource, err := handler.tracker.Init(input.Type, nil, nil)
	if err != nil {
		log.Println("checking failed:", err)
		return
	}

	defer handler.tracker.Release(resource)

	encoder := json.NewEncoder(conn)

	for {
		log.Println("checking", input)

		versions, err := resource.Check(input)
		if err != nil {
			log.Println("checking failed:", err)
			return
		}

		err = encoder.Encode(versions)
		if err != nil {
			log.Println("writing check result failed:", err)
			break
		}

		err = json.NewDecoder(conn).Decode(&input)
		if err == io.EOF {
			break
		}

		if err != nil {
			log.Println("malformed request:", err)
			return
		}
	}
}
