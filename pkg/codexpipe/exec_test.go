package codexpipe

import (
	"strings"
	"testing"
)

func TestParseEvents(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		wantText   string
		wantThread string
		wantErr    string
	}{
		{
			name: "agent message and thread id",
			output: strings.Join([]string{
				`{"type":"thread.started","thread_id":"0196-abc"}`,
				`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"hello"}}`,
				`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":5}}`,
			}, "\n"),
			wantText:   "hello",
			wantThread: "0196-abc",
		},
		{
			name: "multiple agent messages joined",
			output: strings.Join([]string{
				`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"one"}}`,
				`{"type":"item.completed","item":{"id":"i2","type":"agent_message","text":"two"}}`,
			}, "\n"),
			wantText: "one\ntwo",
		},
		{
			name:    "error event with no content",
			output:  `{"type":"error","message":"boom"}`,
			wantErr: "boom",
		},
		{
			name: "malformed lines are skipped",
			output: strings.Join([]string{
				`not json`,
				`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"ok"}}`,
			}, "\n"),
			wantText: "ok",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEvents(tt.output)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseEvents() err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEvents() err = %v, want nil", err)
			}
			if got.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", got.Text, tt.wantText)
			}
			if tt.wantThread != "" && got.ThreadID != tt.wantThread {
				t.Errorf("ThreadID = %q, want %q", got.ThreadID, tt.wantThread)
			}
		})
	}
}
