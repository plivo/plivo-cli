package cmd

import (
	"fmt"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
)

// These tests cover the pure helpers behind `ask -i` (input classification and
// the session's history bookkeeping). They run without a server: the REPL loop
// and SSE streaming are exercised separately by the renderer tests.

func TestClassifyREPLInput(t *testing.T) {
	cases := []struct {
		name       string
		line       string
		wantAction replAction
		wantArg    string
	}{
		{"empty", "", replEmpty, ""},
		{"whitespace only", "   \t ", replEmpty, ""},

		{"exit", "/exit", replExit, ""},
		{"quit", "/quit", replExit, ""},
		{"q short", "/q", replExit, ""},
		{"exit uppercase", "/EXIT", replExit, ""},
		{"exit with trailing space", "  /exit  ", replExit, ""},

		{"reset", "/reset", replReset, ""},
		{"new", "/new", replReset, ""},
		{"reset mixed case", "/Reset", replReset, ""},

		{"help", "/help", replHelp, ""},
		{"help question", "/?", replHelp, ""},

		{"unknown command", "/bogus", replUnknown, "/bogus"},
		{"unknown command with args", "/foo bar", replUnknown, "/foo bar"},

		{"plain message", "what is error 30007", replMessage, "what is error 30007"},
		{"message gets trimmed", "  hello there  ", replMessage, "hello there"},
		{"message that mentions a slash mid-text", "send to /v1/foo", replMessage, "send to /v1/foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotAction, gotArg := classifyREPLInput(tc.line)
			if gotAction != tc.wantAction {
				t.Errorf("action = %d, want %d", gotAction, tc.wantAction)
			}
			if gotArg != tc.wantArg {
				t.Errorf("arg = %q, want %q", gotArg, tc.wantArg)
			}
		})
	}
}

func TestBuddySession_historyForRequest_capsToMax(t *testing.T) {
	s := &buddySession{}
	// Push more than the cap so trimming is exercised.
	total := maxHistoryTurns + 7
	for i := 0; i < total; i++ {
		s.record("user", fmt.Sprintf("turn-%d", i))
	}

	got := s.historyForRequest()
	if len(got) != maxHistoryTurns {
		t.Fatalf("historyForRequest len = %d, want %d", len(got), maxHistoryTurns)
	}
	// It must keep the most recent turns, in order.
	wantFirst := fmt.Sprintf("turn-%d", total-maxHistoryTurns)
	if got[0].Text != wantFirst {
		t.Errorf("first kept turn = %q, want %q", got[0].Text, wantFirst)
	}
	wantLast := fmt.Sprintf("turn-%d", total-1)
	if got[len(got)-1].Text != wantLast {
		t.Errorf("last kept turn = %q, want %q", got[len(got)-1].Text, wantLast)
	}
}

func TestBuddySession_historyForRequest_underCap_returnsAll(t *testing.T) {
	s := &buddySession{}
	s.record("user", "a")
	s.record("assistant", "b")

	got := s.historyForRequest()
	if len(got) != 2 {
		t.Fatalf("historyForRequest len = %d, want 2", len(got))
	}
	if got[0].Text != "a" || got[1].Text != "b" {
		t.Errorf("history order wrong: %+v", got)
	}
}

func TestBuddySession_historyForRequest_empty(t *testing.T) {
	s := &buddySession{}
	if got := s.historyForRequest(); len(got) != 0 {
		t.Errorf("empty session history len = %d, want 0", len(got))
	}
}

func TestBuddySession_record(t *testing.T) {
	s := &buddySession{}
	s.record("user", "question")
	s.record("assistant", "answer")

	want := []api.BuddyTurn{
		{Role: "user", Text: "question"},
		{Role: "assistant", Text: "answer"},
	}
	if len(s.history) != len(want) {
		t.Fatalf("history len = %d, want %d", len(s.history), len(want))
	}
	for i, w := range want {
		if s.history[i].Role != w.Role || s.history[i].Text != w.Text {
			t.Errorf("history[%d] = %+v, want %+v", i, s.history[i], w)
		}
	}
}

func TestBuddySession_reset(t *testing.T) {
	s := &buddySession{}
	s.record("user", "a")
	s.record("assistant", "b")
	s.reset()
	if len(s.history) != 0 {
		t.Errorf("history after reset len = %d, want 0", len(s.history))
	}
	// Recording after a reset should start fresh.
	s.record("user", "c")
	if len(s.history) != 1 || s.history[0].Text != "c" {
		t.Errorf("history after reset+record = %+v, want one turn 'c'", s.history)
	}
}
