package runtime

import (
	"bufio"
	"os"
)

type Runtime struct {
	input *bufio.Reader
}

func New() *Runtime {
	return &Runtime{input: bufio.NewReader(os.Stdin)}
}
