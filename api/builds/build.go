package builds

type Build struct {
	Guid string `json:"guid"`

	Callback string `json:"callback"`

	Source BuildSource `json:"source"`

	Parameters       map[string]string `json:"parameters"`
	SecureParameters map[string]string `json:"secure_parameters"`

	Status string `json:"status"`
}

type BuildSource struct {
	Type string `json:"type"`
	URI  string `json:"uri"`
	Ref  string `json:"ref"`
}
