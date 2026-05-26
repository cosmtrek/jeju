package runtime

import (
	"encoding/json"
	"fmt"
)

type ActionType string

const (
	ActionToolCall ActionType = "tool_call"
	ActionAskUser  ActionType = "ask_user"
	ActionFinal    ActionType = "final"
)

type Action struct {
	Type     ActionType      `json:"type"`
	Thought  string          `json:"thought,omitempty"`
	Tool     string          `json:"tool,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	Question string          `json:"question,omitempty"`
	Content  string          `json:"content,omitempty"`
}

func ParseAction(text string) (Action, error) {
	var action Action
	if err := json.Unmarshal([]byte(text), &action); err != nil {
		return action, err
	}
	switch action.Type {
	case ActionToolCall:
		if action.Tool == "" {
			return action, fmt.Errorf("tool_call missing tool")
		}
		if len(action.Input) == 0 {
			action.Input = json.RawMessage(`{}`)
		}
	case ActionAskUser:
		if action.Question == "" {
			return action, fmt.Errorf("ask_user missing question")
		}
	case ActionFinal:
		if action.Content == "" {
			return action, fmt.Errorf("final missing content")
		}
	default:
		return action, fmt.Errorf("unknown action type %q", action.Type)
	}
	return action, nil
}
