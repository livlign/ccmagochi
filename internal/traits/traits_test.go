package traits

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLateByStandards_NeedsSamples(t *testing.T) {
	tr := empty()
	tr.HourHist[10] = 5
	tr.Samples = 5
	if tr.LateByStandards(23) {
		t.Error("too few samples → no judgment")
	}
}

func TestLateByStandards_PastUsualBand(t *testing.T) {
	tr := empty()
	// usual work: 9–17, lots of samples; almost nothing late.
	for h := 9; h <= 17; h++ {
		tr.HourHist[h] = 20
	}
	tr.Samples = 0
	for h := 0; h < 24; h++ {
		tr.Samples += tr.HourHist[h]
	}
	if !tr.LateByStandards(1) { // 1am is well past a 9–17 band
		t.Error("1am should be late by a 9–17 worker's standards")
	}
	if tr.LateByStandards(14) {
		t.Error("2pm is squarely within the usual band")
	}
}

func TestRareDayOfWeek(t *testing.T) {
	tr := empty()
	tr.SessionCount = 14
	tr.DowHist[int(time.Monday)] = 5
	tr.DowHist[int(time.Tuesday)] = 5
	tr.DowHist[int(time.Sunday)] = 0 // never works Sundays
	if !tr.RareDayOfWeek(time.Sunday) {
		t.Error("Sunday with 0 of 14 sessions is rare")
	}
	if tr.RareDayOfWeek(time.Monday) {
		t.Error("Monday is a normal day here")
	}
}

func TestFavoriteFile_NeedsClearLeader(t *testing.T) {
	tr := empty()
	tr.FileTotal["a.go"] = 40
	tr.FileTotal["b.go"] = 5
	f, ok := tr.FavoriteFile()
	if !ok || f != "a.go" {
		t.Errorf("a.go should be the clear favorite, got %q/%v", f, ok)
	}
	// close race → no favorite
	tr2 := empty()
	tr2.FileTotal["a.go"] = 20
	tr2.FileTotal["b.go"] = 18
	if _, ok := tr2.FavoriteFile(); ok {
		t.Error("a near-tie should not declare a favorite")
	}
}

func TestPaceVerdict(t *testing.T) {
	tr := empty()
	tr.SessionCount = 10
	tr.ToolTotal = 600
	tr.ActiveMs = 10 * 3600000 // 60 calls/hour baseline
	// current session: 200 calls in 1h = 200/hr → much faster
	if v := tr.PaceVerdict(200, 3600000); v != "faster" {
		t.Errorf("want faster, got %q", v)
	}
	// 10 calls in 1h = 10/hr → much slower
	if v := tr.PaceVerdict(10, 3600000); v != "slower" {
		t.Errorf("want slower, got %q", v)
	}
	// normal pace
	if v := tr.PaceVerdict(65, 3600000); v != "" {
		t.Errorf("want normal (\"\"), got %q", v)
	}
}

func TestUsualBreak(t *testing.T) {
	tr := empty()
	for i := 0; i < 10; i++ {
		tr.ObserveBreak(12) // usually breaks at noon
	}
	if !tr.UsualBreak(12) {
		t.Error("noon should be the usual break hour")
	}
	if tr.UsualBreak(15) {
		t.Error("3pm is not the usual break hour")
	}
}

func TestAnniversaryDays(t *testing.T) {
	tr := empty()
	tr.SetFirstSeen(0) // ignored
	day := int64(24 * 3600 * 1000)
	tr.SetFirstSeen(1) // first seen ~epoch
	if _, ok := tr.AnniversaryDays(1 + 30*day); !ok {
		t.Error("day 30 is a milestone")
	}
	if _, ok := tr.AnniversaryDays(1 + 31*day); ok {
		t.Error("day 31 is not a milestone")
	}
}

func TestReadsMoreThanWrites(t *testing.T) {
	tr := empty()
	for i := 0; i < 200; i++ {
		tr.ObserveToolKind("Read")
	}
	for i := 0; i < 20; i++ {
		tr.ObserveToolKind("Edit")
	}
	if !tr.ReadsMoreThanWrites() {
		t.Error("200 reads vs 20 writes → reads more")
	}
}

func TestRevertsAfterCommit(t *testing.T) {
	tr := empty()
	for i := 0; i < 6; i++ {
		tr.ObserveCommit()
	}
	for i := 0; i < 3; i++ {
		tr.ObserveQuickRevert()
	}
	if !tr.RevertsAfterCommit() {
		t.Error("3 quick reverts over 6 commits → quirk")
	}
}

func TestFavoriteRepo(t *testing.T) {
	tr := empty()
	for i := 0; i < 10; i++ {
		tr.ObserveRepo("ccmagotchi")
	}
	tr.ObserveRepo("other")
	if r, ok := tr.FavoriteRepo(); !ok || r != "ccmagotchi" {
		t.Errorf("ccmagotchi should be the home repo, got %q/%v", r, ok)
	}
}

func TestRebuild_FromEvents(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.log")
	// two sessions separated by a >30min gap, with a file + commit.
	lines := []string{
		`{"ts":1000000000000,"type":"tool_call","data":{"name":"Read","file":"a.go"}}`,
		`{"ts":1000000060000,"type":"tool_call","data":{"name":"Edit","file":"a.go"}}`,
		`{"ts":1000005000000,"type":"tool_call","data":{"name":"Read","file":"b.go"}}`, // +~82min → new session
	}
	if err := os.WriteFile(p, []byte(joinLines(lines)), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := Rebuild(p)
	if tr.SessionCount != 2 {
		t.Errorf("gap should segment into 2 sessions, got %d", tr.SessionCount)
	}
	if tr.ReadTools != 2 || tr.WriteTools != 1 {
		t.Errorf("tool kinds: read=%d write=%d", tr.ReadTools, tr.WriteTools)
	}
	if tr.FirstSeenMs != 1000000000000 {
		t.Errorf("first-seen wrong: %d", tr.FirstSeenMs)
	}
}

func joinLines(ls []string) string {
	out := ""
	for _, l := range ls {
		out += l + "\n"
	}
	return out
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "traits.json")
	tr := empty()
	tr.ObserveEvent("tool_call", 14)
	tr.ObserveFile("x.go")
	if err := tr.Save(p); err != nil {
		t.Fatal(err)
	}
	got := Load(p)
	if got.ToolTotal != 1 || got.FileTotal["x.go"] != 1 || got.HourHist[14] != 1 {
		t.Errorf("traits did not survive round-trip: %+v", got)
	}
}
