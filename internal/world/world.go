// Package world is the third layer (pet-world.md): the horizontal space the dog
// lives in. It owns terrain width/tiers, the dog's column position, anchored
// scenery, ambient text, and subagent robots. Everything here stays put when
// the dog moves — the dog walks through the world, the world doesn't follow.
//
// The daemon owns world *dynamics* (where the dog/scenery/robots are); this
// package is the pure painter: given positioned pieces and a width, it lays out
// one line. The renderer calls Compose each refresh.
package world

import (
	"os"
	"strconv"
	"strings"
)

const (
	reserveRight = 30 // CC writes notifications on the right of line 2 — keep clear
	minUsable    = 12 // below this: compact mode, dog only
	dimAnsi      = "\x1b[2;37m"
	reset        = "\x1b[0m"
)

// Cols reads COLUMNS from the environment (the renderer's terminal width),
// falling back to 80.
func Cols() int {
	if v := os.Getenv("COLUMNS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 80
}

// Usable is the world width after reserving the right notification margin.
func Usable(cols int) int {
	if u := cols - reserveRight; u > 0 {
		return u
	}
	return 0
}

// Tier names the layout band (pet-world §2).
func Tier(cols int) string {
	switch {
	case cols < 50:
		return "compact"
	case cols < 80:
		return "yard"
	case cols < 120:
		return "garden"
	default:
		return "park"
	}
}

// MaxRobots is how many subagent robots a tier shows before collapsing to +N.
func MaxRobots(cols int) int {
	switch Tier(cols) {
	case "yard":
		return 1
	case "garden":
		return 2
	case "park":
		return 3
	default:
		return 0 // compact: none, just +N
	}
}

// Width is the visible column width of a string: ANSI escapes are zero-width,
// emoji (and other astral glyphs) count as 2, everything else as 1.
func Width(s string) int {
	w := 0
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		if r[i] == 0x1b { // skip an ANSI escape: ESC [ ... m
			for i < len(r) && r[i] != 'm' {
				i++
			}
			continue
		}
		if r[i] >= 0x1F300 {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// Scenery is one anchored world object.
type Scenery struct {
	Glyph string `json:"glyph"`
	Pos   int    `json:"pos"`
}

// Item is a positioned, pre-rendered (already-colored) piece for layout.
type Item struct {
	Col, W int
	S      string
}

// Compose lays out the full pet line: the dog at its column among scenery and
// robots, with ambient text right-anchored. In compact terminals it returns
// just the dog (+ a "+N" subagent count). robotOverflow is the count beyond
// what the tier shows.
func Compose(cols int, dog string, dogPos int, scenery []Scenery, robots []Item, robotOverflow int, ambient string) string {
	usable := Usable(cols)
	dogW := Width(dog)
	if cols < 50 || usable < minUsable { // compact tier: dog only, subagents as +N
		if n := robotOverflow + len(robots); n > 0 {
			return dog + " +" + strconv.Itoa(n)
		}
		return dog
	}

	// clamp the dog into the world
	if dogPos < 0 {
		dogPos = 0
	}
	if dogPos > usable-dogW {
		dogPos = usable - dogW
	}
	if dogPos < 0 {
		dogPos = 0
	}

	items := make([]Item, 0, len(scenery)+len(robots)+3)
	items = append(items, Item{Col: dogPos, W: dogW, S: dog})
	for _, s := range scenery {
		if s.Glyph == "" {
			continue
		}
		sw := Width(s.Glyph)
		// Occlusion: the dog stands IN FRONT of scenery it overlaps. Skip scenery
		// whose columns fall within the dog's span so anchored objects stay put
		// (hidden under the dog, reappearing when it walks off) instead of being
		// shoved sideways and appearing to travel with the dog (pet-world §4: the
		// world doesn't move with the dog).
		if s.Pos < dogPos+dogW && s.Pos+sw > dogPos {
			continue
		}
		items = append(items, Item{Col: s.Pos, W: sw, S: s.Glyph})
	}
	items = append(items, robots...)
	if robotOverflow > 0 {
		tag := "+" + strconv.Itoa(robotOverflow)
		items = append(items, Item{Col: dogPos + dogW + 1, W: Width(tag), S: tag})
	}
	if ambient != "" {
		aw := Width(ambient)
		if col := usable - aw; col > dogPos+dogW {
			items = append(items, Item{Col: col, W: aw, S: dimAnsi + ambient + reset})
		}
	}
	return layout(items, usable)
}

// layout sorts items by column and paints them left-to-right, padding gaps with
// spaces. Overlaps are forgiving: a colliding item is pushed right of the
// cursor (pet-world §10 — best-effort, no physics). Items past the edge drop.
func layout(items []Item, usable int) string {
	for i := 1; i < len(items); i++ { // insertion sort (tiny n, stable enough)
		for j := i; j > 0 && items[j].Col < items[j-1].Col; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	var b strings.Builder
	cursor := 0
	for _, it := range items {
		if it.S == "" {
			continue
		}
		c := it.Col
		if c < cursor {
			c = cursor // pushed right on collision
		}
		if cursor > 0 && c == cursor {
			c = cursor + 1 // never let two items touch (e.g. scenery abutting ambient)
		}
		if c >= usable {
			continue // beyond the world edge
		}
		if c > cursor {
			b.WriteString(strings.Repeat(" ", c-cursor))
		}
		b.WriteString(it.S)
		cursor = c + it.W
	}
	return strings.TrimRight(b.String(), " ")
}
