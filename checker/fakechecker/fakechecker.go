package fakechecker

import (
	"encoding/json"
	"sync"

	"github.com/winston-ci/prole/api/builds"
)

type FakeChecker struct {
	checked      []builds.Input
	checkedMutex *sync.Mutex
	CheckResult  []*json.RawMessage
	CheckError   error
}

func New() *FakeChecker {
	return &FakeChecker{
		checkedMutex: new(sync.Mutex),
	}
}

func (checker *FakeChecker) Check(input builds.Input) ([]*json.RawMessage, error) {
	if checker.CheckError != nil {
		return nil, checker.CheckError
	}

	checker.checkedMutex.Lock()
	checker.checked = append(checker.checked, input)
	checker.checkedMutex.Unlock()

	return checker.CheckResult, nil
}

func (checker *FakeChecker) Checked() []builds.Input {
	checker.checkedMutex.Lock()
	defer checker.checkedMutex.Unlock()

	return checker.checked
}
