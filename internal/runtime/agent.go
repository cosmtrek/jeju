package runtime

import (
	"bufio"
	"io"
	"os"
)

type Runtime struct {
	input         *bufio.Reader
	autoApprove   bool
	autoUserInput *string
}

type Options struct {
	Input         io.Reader
	AutoApprove   bool
	AutoUserInput *string
}

func New() *Runtime {
	return NewWithOptions(Options{})
}

func NewWithOptions(opts Options) *Runtime {
	input := opts.Input
	if input == nil {
		input = os.Stdin
	}
	return &Runtime{
		input:         bufio.NewReader(input),
		autoApprove:   opts.AutoApprove,
		autoUserInput: opts.AutoUserInput,
	}
}
