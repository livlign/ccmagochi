// Package quirks seeds a fixed, per-pet identity ONCE (on first run) and
// persists it to quirks.json. Never regenerated — this is the "you can't get
// this exact pet again" element from idea.md.
package quirks

import (
	"encoding/json"
	"math/rand"
	"os"
	"time"
)

type Quirks struct {
	FavoriteExt   string `json:"favorite_ext"`   // reacts cheekily when you touch a new file of this type
	PreferredHour int    `json:"preferred_hour"` // (reserved) a subtly-preferred working hour
	SpeechTic     string `json:"speech_tic"`     // occasionally tacked onto a remark — its verbal fingerprint
	Aversion      string `json:"aversion"`       // one tool it reacts to with mild distress
	Seeded        bool   `json:"seeded"`
}

var (
	exts      = []string{".go", ".ts", ".tsx", ".py", ".rs", ".js", ".jsx", ".md", ".sql", ".css", ".json", ".sh", ".cs"}
	tics      = []string{" ~", "!", "…", ", hmm", " — y'know?", " (as usual)", ", I guess", " :)"}
	aversions = []string{"Grep", "Glob", "WebFetch", "WebSearch"} // deliberately the rarer tools
)

// Load returns the persisted quirks, seeding them once on first run.
func Load(path string) Quirks {
	var q Quirks
	if b, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(b, &q) == nil && q.Seeded {
			return q // already minted — never changes
		}
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	q = Quirks{
		FavoriteExt:   exts[r.Intn(len(exts))],
		PreferredHour: r.Intn(24),
		SpeechTic:     tics[r.Intn(len(tics))],
		Aversion:      aversions[r.Intn(len(aversions))],
		Seeded:        true,
	}
	if b, err := json.MarshalIndent(q, "", "  "); err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
	return q
}
