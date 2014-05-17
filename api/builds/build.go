package builds

type Build struct {
	Guid string `json:"guid"`

	Config BuildConfig `json:"config"`

	LogsURL  string `json:"logs_url"`
	Callback string `json:"callback"`

	Sources []BuildSource `json:"sources"`

	Status string `json:"status"`
}

type BuildConfig struct {
	Image string `json:"image"`

	Env    [][2]string `json:"env"`
	Script string      `json:"script"`

	Privileged bool `json:"privileged"`
}

type BuildSource struct {
	Type string `json:"type"`

	ConfigPath      string `json:"configPath"`
	DestinationPath string `json:"destinationPath"`
}
