package quirks

import (
	"path/filepath"
	"testing"
)

func TestLoad_SeedsOnceAndPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "quirks.json")
	q1 := Load(p)
	if !q1.Seeded || q1.FavoriteExt == "" || q1.SpeechTic == "" || q1.Aversion == "" {
		t.Fatalf("first load should seed a full identity: %+v", q1)
	}
	q2 := Load(p)
	if q2 != q1 {
		t.Errorf("identity must never change once seeded:\n  %+v\n  %+v", q1, q2)
	}
}
