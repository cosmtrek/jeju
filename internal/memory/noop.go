package memory

type Noop struct{}

func (Noop) Enabled() bool {
	return false
}
