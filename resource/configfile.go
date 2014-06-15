package resource

type ConfigFile struct {
	Image  string              `yaml:"image"`
	Env    []map[string]string `yaml:"env"`
	Script string              `yaml:"script"`
}
