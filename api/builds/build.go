package builds

type Status string

const (
	StatusStarted   Status = "started"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusErrored   Status = "errored"
	StatusAborted   Status = "aborted"
)

type Build struct {
	Guid string `json:"guid"`

	Privileged bool `json:"privileged"`

	Config Config `json:"config"`

	Inputs  []Input  `json:"inputs"`
	Outputs []Output `json:"outputs"`

	AbortURL  string `json:"abort_url"`
	HijackURL string `json:"hijack_url"`
}

type Config struct {
	Image  string            `json:"image,omitempty"   yaml:"image"`
	Params map[string]string `json:"params,omitempty"  yaml:"params"`
	Run    RunConfig         `json:"run,omitempty"     yaml:"run"`
	Inputs []InputConfig     `json:"inputs,omitempty"  yaml:"inputs"`
}

type RunConfig struct {
	Path string   `json:"path" yaml:"path"`
	Args []string `json:"args,omitempty" yaml:"args"`
}

type InputConfig struct {
	Name string `json:"name" yaml:"name"`
	Path string `json:"path,omitempty" yaml:"path"`
}

type Input struct {
	Name string `json:"name"`

	Type string `json:"type"`

	// e.g. sha
	Version Version `json:"version,omitempty"`

	// e.g. git url, branch, private_key
	Source Source `json:"source,omitempty"`

	// arbitrary config for input
	Params Params `json:"params,omitempty"`

	// e.g. commit_author, commit_date
	Metadata []MetadataField `json:"metadata,omitempty"`

	ConfigPath string `json:"config_path"`
}

type Version map[string]interface{}

type MetadataField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Output struct {
	Name string `json:"name"`

	Type string `json:"type"`

	// e.g. [success, failure]
	On OutputConditions `json:"on,omitempty"`

	// e.g. sha
	Version Version `json:"version,omitempty"`

	// e.g. git url, branch, private_key
	Source Source `json:"source,omitempty"`

	// arbitrary config for output
	Params Params `json:"params,omitempty"`

	// e.g. commit_author, commit_date, commit_sha
	Metadata []MetadataField `json:"metadata,omitempty"`
}

type OutputConditions []OutputCondition

func (cs OutputConditions) SatisfiedBy(exitStatus int) bool {
	for _, status := range cs {
		if (status == OutputConditionSuccess && exitStatus == 0) ||
			(status == OutputConditionFailure && exitStatus != 0) {
			return true
		}
	}

	return false
}

type OutputCondition string

const (
	OutputConditionSuccess OutputCondition = "success"
	OutputConditionFailure OutputCondition = "failure"
)

type Source map[string]interface{}

type Params map[string]interface{}
