package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cosmtrek/jeju/internal/tools"
)

type Tool struct {
	spec tools.Spec
}

func New(spec tools.Spec) *Tool {
	return &Tool{spec: spec}
}

func (t *Tool) Name() string {
	return t.spec.Name
}

func (t *Tool) Spec() tools.Spec {
	return t.spec
}

func (t *Tool) Run(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	return tools.Result{}, fmt.Errorf("agent tool %q must be executed by the runtime", t.spec.Name)
}
