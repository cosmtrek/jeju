package runtime

import "testing"

func TestParseActionFinal(t *testing.T) {
	action, err := ParseAction(`{"type":"final","thought":"done","content":"ok"}`)
	if err != nil {
		t.Fatalf("ParseAction returned error: %v", err)
	}
	if action.Type != ActionFinal || action.Content != "ok" {
		t.Fatalf("unexpected action: %#v", action)
	}
}

func TestParseActionRejectsUnknownType(t *testing.T) {
	if _, err := ParseAction(`{"type":"other"}`); err == nil {
		t.Fatal("expected error for unknown action type")
	}
}
