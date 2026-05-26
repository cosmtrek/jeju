package runtime

import (
	"fmt"
	"time"

	"jeju/internal/config"
)

func shouldStop(state *RunState, limits config.RuntimeLimits) error {
	if state.IsTerminal() {
		return nil
	}
	if limits.MaxSteps > 0 && state.Step >= limits.MaxSteps {
		return fmt.Errorf("max steps exceeded: %d", limits.MaxSteps)
	}
	if limits.MaxToolCalls > 0 && state.ToolCalls >= limits.MaxToolCalls {
		return fmt.Errorf("max tool calls exceeded: %d", limits.MaxToolCalls)
	}
	if limits.MaxConsecutiveErrors > 0 && state.ConsecutiveErrors >= limits.MaxConsecutiveErrors {
		return fmt.Errorf("max consecutive errors exceeded: %d", limits.MaxConsecutiveErrors)
	}
	if limits.MaxDurationSec > 0 && time.Since(state.StartedAt) > time.Duration(limits.MaxDurationSec)*time.Second {
		return fmt.Errorf("max duration exceeded: %ds", limits.MaxDurationSec)
	}
	return nil
}
