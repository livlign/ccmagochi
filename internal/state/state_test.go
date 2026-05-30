package state

import (
	"path/filepath"
	"testing"
)

func TestWriteReadNow_RoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "now.json")
	in := Now{Mood: Mood{Stress: 0.5, Curiosity: 0.3}, Activity: "thinking", Remark: &Remark{Text: "hi", ExpiresMs: 9}}
	if err := WriteNow(p, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadNow(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Activity != "thinking" || out.Mood.Stress != 0.5 || out.Remark == nil || out.Remark.Text != "hi" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestSession_RoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	if err := WriteSession(p, Session{TranscriptPath: "/x.jsonl", SessionID: "id1"}); err != nil {
		t.Fatal(err)
	}
	s, err := ReadSession(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.TranscriptPath != "/x.jsonl" || s.SessionID != "id1" {
		t.Fatalf("got %+v", s)
	}
}

func TestRemarked_RecencyWindow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "r.log")
	for i, txt := range []string{"a", "b", "c"} {
		if err := AppendRemarked(p, "cat", txt, int64(i)); err != nil {
			t.Fatal(err)
		}
	}
	got := RecentRemarks(p, 2)
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("want last 2 [b c], got %v", got)
	}
}

func TestReadNow_MissingFile(t *testing.T) {
	if _, err := ReadNow(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("expected error for missing now.json")
	}
}
