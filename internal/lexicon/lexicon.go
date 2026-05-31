// Package lexicon is the pet's growing vocabulary — the one part of its speech
// that evolves with your usage. As you prompt, it harvests two things from your
// text: which casual "flavor" words you favor (from a curated, safe whitelist —
// never arbitrary tokens), and your tone (terse vs chatty, exclamatory or calm).
//
// The hybrid model (idea.md principle 5, mildly relaxed): the pet keeps its own
// deadpan cadence, but sprinkles in YOUR favored interjections and matches your
// tone — so it sounds a little more like you over time without becoming a mirror.
//
// Privacy: only derived COUNTS persist (lexicon.json). Raw prompt text is never
// stored, and the flavor pool is a fixed whitelist, so no path, name, or secret
// from a prompt can ever be echoed back.
package lexicon

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

// promptsReady is how many prompts before tone/flavor is trusted (so the pet
// doesn't adopt a style from two messages).
const promptsReady = 15

// flavorUseFloor is how many times you must have used a word before the pet
// will echo it — this is the "grows with usage" gate.
const flavorUseFloor = 3

// flavorSet is the curated, safe pool of casual interjections the pet may adopt.
// Tone/filler words only — nothing that could carry sensitive content.
var flavorSet = map[string]bool{
	"lol": true, "haha": true, "lmao": true, "yeah": true, "yep": true, "yup": true,
	"nope": true, "nah": true, "ok": true, "okay": true, "cool": true, "nice": true,
	"sweet": true, "ship": true, "ugh": true, "hmm": true, "huh": true, "oops": true,
	"wait": true, "actually": true, "basically": true, "honestly": true, "literally": true,
	"just": true, "please": true, "thanks": true, "thx": true, "damn": true, "dang": true,
	"welp": true, "oof": true, "yikes": true, "sigh": true, "woo": true, "aha": true,
	"right": true, "sure": true, "kinda": true, "gonna": true, "wanna": true, "fr": true,
	"tbh": true, "imo": true, "btw": true, "anyway": true, "alright": true, "neat": true,
}

// Lexicon is the persisted, forward-accumulating store.
type Lexicon struct {
	Prompts   int            `json:"prompts"`   // total prompts observed
	Words     int            `json:"words"`     // total word tokens (→ avg length)
	Exclaims  int            `json:"exclaims"`  // total '!' across prompts
	Questions int            `json:"questions"` // total '?' across prompts
	Flavor    map[string]int `json:"flavor"`    // whitelisted interjection -> times you used it
}

func empty() Lexicon { return Lexicon{Flavor: map[string]int{}} }

// Load reads lexicon.json, returning an empty (initialized) store on any error.
func Load(path string) Lexicon {
	l := empty()
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &l)
		if l.Flavor == nil {
			l.Flavor = map[string]int{}
		}
	}
	return l
}

// Save writes lexicon.json atomically.
func (l *Lexicon) Save(path string) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Observe folds one user prompt into the lexicon. Only counts are kept; the text
// itself is discarded. Long prompts are capped so harvesting stays cheap.
func (l *Lexicon) Observe(text string) {
	if text == "" {
		return
	}
	if len(text) > 4000 {
		text = text[:4000]
	}
	l.Prompts++
	l.Exclaims += strings.Count(text, "!")
	l.Questions += strings.Count(text, "?")
	for _, tok := range tokenize(text) {
		l.Words++
		if flavorSet[tok] {
			l.Flavor[tok]++
		}
	}
}

// tokenize lowercases and splits on non-letters, yielding alphabetic word tokens.
func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
}

// --- derived signals ---

// Ready reports whether enough prompts have been seen to trust tone/flavor.
func (l *Lexicon) Ready() bool { return l.Prompts >= promptsReady }

// FlavorWords returns the user's favored interjections (used ≥ floor times),
// most-used first, capped at n. Empty until Ready.
func (l *Lexicon) FlavorWords(n int) []string {
	if !l.Ready() {
		return nil
	}
	type wc struct {
		w string
		c int
	}
	var all []wc
	for w, c := range l.Flavor {
		if c >= flavorUseFloor {
			all = append(all, wc{w, c})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].c != all[j].c {
			return all[i].c > all[j].c
		}
		return all[i].w < all[j].w // stable
	})
	out := make([]string, 0, n)
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, all[i].w)
	}
	return out
}

// Terse reports whether you tend to write short prompts (your remarks get trimmed).
func (l *Lexicon) Terse() bool {
	if !l.Ready() {
		return false
	}
	return float64(l.Words)/float64(l.Prompts) < 6.0
}

// Exclaim reports whether you punctuate energetically (the pet matches with "!").
func (l *Lexicon) Exclaim() bool {
	if !l.Ready() {
		return false
	}
	return l.Exclaims*2 > l.Prompts // > 0.5 '!' per prompt on average
}
