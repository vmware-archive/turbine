package turbine

import garden "github.com/cloudfoundry-incubator/garden/api"

type HijackPayload struct {
	Stdin   []byte
	TTYSpec *garden.TTYSpec
}
