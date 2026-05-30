package transcript

import (
	"strings"
	"testing"
)

// Lines crafted to match the Phase-0 probe-confirmed schema.
func TestClassify_FullFlow(t *testing.T) {
	c := NewClassifier()
	lines := []string{
		`{"type":"user","timestamp":"2026-05-30T00:00:00.000Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-05-30T00:00:30.000Z","message":{"role":"assistant","content":[{"type":"thinking"},{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"/a/b.go"}}]}}`,
		`{"type":"assistant","timestamp":"2026-05-30T00:00:40.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Agent","input":{"subagent_type":"Explore"}}]}}`,
		`{"type":"user","timestamp":"2026-05-30T00:00:45.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1"}]}}`,
		`{"type":"user","timestamp":"2026-05-30T00:01:50.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2"}]}}`,
		`{"type":"file-history-snapshot","timestamp":"2026-05-30T00:02:00.000Z"}`, // meta → ignored
	}
	var got []string
	for _, l := range lines {
		for _, e := range c.Classify([]byte(l)) {
			got = append(got, e.Type)
		}
	}
	want := "prompt_submit,thinking_turn,tool_call,subagent_spawn,tool_done,subagent_done"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %q\nwant %q", strings.Join(got, ","), want)
	}
}

func TestClassify_DevEvents(t *testing.T) {
	bashTypes := func(cmd string) []string {
		c := NewClassifier()
		var types []string
		for _, e := range c.Classify([]byte(`{"type":"assistant","timestamp":"2026-05-31T00:00:00.000Z","message":{"content":[{"type":"tool_use","id":"b","name":"Bash","input":{"command":"` + cmd + `"}}]}}`)) {
			types = append(types, e.Type)
		}
		return types
	}
	if !contains(bashTypes("git commit -m x"), "commit") {
		t.Error("git commit → commit event")
	}
	if !contains(bashTypes("git restore ."), "revert") {
		t.Error("git restore → revert event")
	}

	// test pass/fail from the result's is_error flag (exit code proxy)
	resultTypes := func(cmd string, isErr bool) []string {
		c := NewClassifier()
		c.Classify([]byte(`{"type":"assistant","timestamp":"2026-05-31T00:00:00.000Z","message":{"content":[{"type":"tool_use","id":"t","name":"Bash","input":{"command":"` + cmd + `"}}]}}`))
		errField := ""
		if isErr {
			errField = `,"is_error":true`
		}
		var types []string
		for _, e := range c.Classify([]byte(`{"type":"user","timestamp":"2026-05-31T00:00:05.000Z","message":{"content":[{"type":"tool_result","tool_use_id":"t"` + errField + `}]}}`)) {
			types = append(types, e.Type)
		}
		return types
	}
	if !contains(resultTypes("go test ./...", false), "test_pass") {
		t.Error("clean test result → test_pass")
	}
	if !contains(resultTypes("pytest", true), "test_fail") {
		t.Error("errored test result → test_fail")
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestClassify_IgnoresMeta(t *testing.T) {
	c := NewClassifier()
	for _, mt := range []string{"mode", "permission-mode", "ai-title", "last-prompt", "attachment"} {
		if ev := c.Classify([]byte(`{"type":"` + mt + `","timestamp":"2026-05-30T00:00:00.000Z"}`)); ev != nil {
			t.Errorf("%s should be ignored, got %v", mt, ev)
		}
	}
}

func TestClassify_ToolError(t *testing.T) {
	c := NewClassifier()
	c.Classify([]byte(`{"type":"assistant","timestamp":"2026-05-30T00:00:00.000Z","message":{"content":[{"type":"tool_use","id":"x","name":"Bash"}]}}`))
	ev := c.Classify([]byte(`{"type":"user","timestamp":"2026-05-30T00:00:02.000Z","message":{"content":[{"type":"tool_result","tool_use_id":"x","is_error":true}]}}`))
	if len(ev) != 1 || ev[0].Type != "error" {
		t.Fatalf("want one error event, got %v", ev)
	}
}

func TestClassify_OpenAgentsTracked(t *testing.T) {
	c := NewClassifier()
	c.Classify([]byte(`{"type":"assistant","timestamp":"2026-05-30T00:00:00.000Z","message":{"content":[{"type":"tool_use","id":"a","name":"Agent","input":{"subagent_type":"Explore"}}]}}`))
	if c.OpenAgents() != 1 {
		t.Fatalf("want 1 open agent, got %d", c.OpenAgents())
	}
	c.Classify([]byte(`{"type":"user","timestamp":"2026-05-30T00:00:10.000Z","message":{"content":[{"type":"tool_result","tool_use_id":"a"}]}}`))
	if c.OpenAgents() != 0 {
		t.Fatalf("agent should be closed, got %d", c.OpenAgents())
	}
}
