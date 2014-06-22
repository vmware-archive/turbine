package check

import (
	"encoding/json"
	"io"
	"log"

	"code.google.com/p/go.net/websocket"

	"github.com/winston-ci/prole/api/builds"
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
		log.Println("malformed request:", err)
		return
	}

	log.Printf("streaming checks for %s (type: %s)\n", input.Name, input.Type)

	resource, err := handler.tracker.Init(input.Type, nil, nil)
	if err != nil {
		log.Println("checking failed:", err)
		return
	}

	defer handler.tracker.Release(resource)

	encoder := json.NewEncoder(conn)

	for {
		log.Printf("checking %s (type: %s)\n", input.Name, input.Type)

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
			select {
			case <-handler.drain:
			default:
				log.Println("malformed request:", err)
			}

			return
		}
	}
}
