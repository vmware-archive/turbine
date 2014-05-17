package checker

import (
	"encoding/json"

	"github.com/winston-ci/prole/api/builds"
)

type Checker interface {
	Check(builds.Input) ([]*json.RawMessage, error)
}
