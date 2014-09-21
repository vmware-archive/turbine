package event

// not an event; used in Log and Error
type Origin struct {
	Type OriginType `json:"type"`
	Name string     `json:"name"`
}

type OriginType string

const (
	OriginTypeInvalid OriginType = ""
	OriginTypeInput   OriginType = "input"
	OriginTypeOutput  OriginType = "output"
	OriginTypeRun     OriginType = "run"
)
