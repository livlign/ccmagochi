package lexicon

import (
	"path/filepath"
	"testing"
)

func TestObserve_HarvestsFlavorNotArbitrary(t *testing.T) {
	l := empty()
	// "auth" and "/secret/path" are NOT in the whitelist → never harvested.
	l.Observe("yeah lets ship the auth thing in /secret/path lol")
	if l.Flavor["yeah"] != 1 || l.Flavor["ship"] != 1 || l.Flavor["lol"] != 1 {
		t.Errorf("whitelisted flavor words should be counted: %+v", l.Flavor)
	}
	if _, ok := l.Flavor["auth"]; ok {
		t.Error("arbitrary words must NOT be harvested (privacy)")
	}
	if _, ok := l.Flavor["secret"]; ok {
		t.Error("path/secret tokens must NOT be harvested")
	}
}

func TestFlavorWords_GrowsWithUsageAndReadiness(t *testing.T) {
	l := empty()
	// not ready yet
	for i := 0; i < 5; i++ {
		l.Observe("lol lol")
	}
	if l.FlavorWords(5) != nil {
		t.Error("flavor should be empty before enough prompts (readiness)")
	}
	// reach readiness; "lol" used many times, "ok" a few
	for i := 0; i < 15; i++ {
		l.Observe("lol ok")
	}
	got := l.FlavorWords(5)
	if len(got) == 0 || got[0] != "lol" {
		t.Errorf("most-used flavor word should lead, got %v", got)
	}
}

func TestTone_TerseAndExclaim(t *testing.T) {
	terse := empty()
	for i := 0; i < 20; i++ {
		terse.Observe("fix this") // 2 words/prompt
	}
	if !terse.Terse() {
		t.Error("short prompts → terse")
	}
	if terse.Exclaim() {
		t.Error("no '!' → not exclaim")
	}

	loud := empty()
	for i := 0; i < 20; i++ {
		loud.Observe("please make this work now thanks so much really appreciate it!!")
	}
	if loud.Terse() {
		t.Error("long prompts → not terse")
	}
	if !loud.Exclaim() {
		t.Error("lots of '!' → exclaim")
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "lexicon.json")
	l := empty()
	l.Observe("yeah cool ship it")
	if err := l.Save(p); err != nil {
		t.Fatal(err)
	}
	got := Load(p)
	if got.Flavor["ship"] != 1 || got.Prompts != 1 {
		t.Errorf("lexicon did not survive round-trip: %+v", got)
	}
}
