package model

import (
	"encoding/json"
)

func (c ToolCall) MarshalJSON() ([]byte, error) {
	type function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	type wire struct {
		ID       string   `json:"id,omitempty"`
		Type     string   `json:"type"`
		Function function `json:"function"`
	}
	args := c.Arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	encodedArgs, err := json.Marshal(string(args))
	if err != nil {
		return nil, err
	}
	return json.Marshal(wire{
		ID:   c.ID,
		Type: "function",
		Function: function{
			Name:      c.Name,
			Arguments: encodedArgs,
		},
	})
}

func (c *ToolCall) UnmarshalJSON(data []byte) error {
	type function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	type wire struct {
		ID       string          `json:"id"`
		Type     string          `json:"type"`
		Function function        `json:"function"`
		Name     string          `json:"name"`
		Args     json.RawMessage `json:"arguments"`
	}
	var raw wire
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.ID = raw.ID
	c.Name = raw.Function.Name
	c.Arguments = decodeArguments(raw.Function.Arguments)
	if c.Name == "" {
		c.Name = raw.Name
		c.Arguments = decodeArguments(raw.Args)
	}
	if len(c.Arguments) == 0 {
		c.Arguments = json.RawMessage(`{}`)
	}
	return nil
}

func decodeArguments(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text == "" {
			return json.RawMessage(`{}`)
		}
		return json.RawMessage(text)
	}
	return raw
}
