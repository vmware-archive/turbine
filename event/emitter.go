package event

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const VERSION = "1.0"

type Emitter interface {
	EmitEvent(Event)
	Close()
}

type NullEmitter struct{}

func (NullEmitter) EmitEvent(Event) {}
func (NullEmitter) Close()          {}

type websocketEmitter struct {
	consumer string
	auth     string

	drain <-chan struct{}

	dialer *websocket.Dialer

	conn  *websocket.Conn
	connL *sync.Mutex

	writeL *sync.Mutex
}

func NewWebSocketEmitter(consumer string, drain <-chan struct{}) Emitter {
	consumerURL, err := url.Parse(consumer)
	if err != nil {
		// should not make it this far; API validates url
		panic("invalid emitter consumer url: " + err.Error())
	}

	// strip basic auth from URL that we use
	var auth string
	if consumerURL.User != nil {
		auth = basicAuth(consumerURL.User)
	}

	consumerURL.User = nil

	return &websocketEmitter{
		consumer: consumerURL.String(),
		auth:     auth,

		drain: drain,

		dialer: &websocket.Dialer{
			// allow detection of failed writes
			//
			// ideally this would be zero, but gorilla uses that to fill in its own
			// default of 4096 :(
			WriteBufferSize: 1,
		},

		connL: new(sync.Mutex),

		writeL: new(sync.Mutex),
	}
}

func (e *websocketEmitter) EmitEvent(event Event) {
	for {
		if !e.connect() {
			return
		}

		e.writeL.Lock()

		err := e.conn.WriteJSON(Message{
			Event: event,
		})

		e.writeL.Unlock()

		if err == nil {
			break
		}

		e.close()

		select {
		case <-time.After(time.Second):
		case <-e.drain:
			return
		}
	}
}

func (e *websocketEmitter) Close() {
	e.close()
}

func (e *websocketEmitter) connect() bool {
	e.connL.Lock()
	defer e.connL.Unlock()

	if e.conn != nil {
		return true
	}

	var err error

	headers := http.Header{}
	if e.auth != "" {
		headers.Set("Authorization", e.auth)
	}

	for {
		e.conn, _, err = e.dialer.Dial(e.consumer, headers)
		if err == nil {
			err = e.conn.WriteJSON(VersionMessage{
				Version: VERSION,
			})
			if err == nil {
				return true
			}
		}

		select {
		case <-time.After(time.Second):
		case <-e.drain:
			return false
		}
	}
}

func (e *websocketEmitter) close() {
	e.connL.Lock()
	defer e.connL.Unlock()

	if e.conn != nil {
		conn := e.conn
		e.conn = nil
		conn.Close()
	}
}

func basicAuth(user *url.Userinfo) string {
	username := user.Username()
	password, _ := user.Password()
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}
