package agent

type bi struct {
	zh string
	en string
}

func (b bi) pick(en bool) string {
	if en {
		return b.en
	}
	return b.zh
}
