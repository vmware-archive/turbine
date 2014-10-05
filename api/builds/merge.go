package builds

func (a Config) Merge(b Config) Config {
	if b.Image != "" {
		a.Image = b.Image
	}

	if len(a.Params) > 0 {
		newParams := map[string]string{}

		for k, v := range a.Params {
			newParams[k] = v
		}

		for k, v := range b.Params {
			newParams[k] = v
		}

		a.Params = newParams
	} else {
		a.Params = b.Params
	}

	if len(b.Inputs) != 0 {
		a.Inputs = b.Inputs
	}

	if b.Run.Path != "" {
		a.Run = b.Run
	}

	return a
}
