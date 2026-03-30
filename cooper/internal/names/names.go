package names

import (
	"fmt"
	"math/rand/v2"
	"sync"
)

// wordList contains one-word terms related to coopering, whiskey aging,
// wine aging, and distillery operations.
var wordList = []string{
	"rickhouse",
	"bung",
	"stave",
	"charring",
	"toasting",
	"vatting",
	"nosing",
	"cask",
	"hogshead",
	"firkin",
	"kilderkin",
	"tun",
	"wort",
	"mash",
	"malting",
	"peating",
	"vatted",
	"chill",
	"proof",
	"dram",
	"nip",
	"measure",
	"cooper",
	"joiner",
	"hooper",
	"croze",
	"chime",
	"bilge",
	"bunghole",
	"adze",
	"drawknife",
	"flagging",
	"raising",
	"trussing",
	"heading",
	"racking",
	"angels",
	"share",
	"thief",
	"valinch",
	"stillage",
	"marrying",
	"finishing",
	"solera",
	"potstill",
	"column",
	"condenser",
	"lyne",
	"worm",
	"washback",
	"feints",
	"middlecut",
	"foreshot",
	"barrel",
	"cooperage",
	"billet",
	"stencil",
	"gauger",
	"ullage",
}

// activeNames maps name -> workspace for all currently active names.
var activeNames sync.Map

// Generate picks a random name not currently active and registers it
// for the given workspace. If all base names are exhausted, it appends
// an incrementing number (e.g. "rickhouse2", "rickhouse3").
func Generate(workspace string) string {
	// Build a shuffled copy of the word list.
	shuffled := make([]string, len(wordList))
	copy(shuffled, wordList)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	// Try each base name.
	for _, name := range shuffled {
		if _, loaded := activeNames.LoadOrStore(name, workspace); !loaded {
			return name
		}
	}

	// All base names exhausted — append incrementing numbers.
	for _, name := range shuffled {
		for n := 2; ; n++ {
			candidate := fmt.Sprintf("%s%d", name, n)
			if _, loaded := activeNames.LoadOrStore(candidate, workspace); !loaded {
				return candidate
			}
		}
	}

	// Unreachable: the inner loop above will always find a candidate.
	return ""
}

// Release marks a name as no longer active, freeing it for reuse.
func Release(name string) {
	activeNames.Delete(name)
}

// IsActive reports whether the given name is currently in use.
func IsActive(name string) bool {
	_, ok := activeNames.Load(name)
	return ok
}

// ActiveNames returns a snapshot copy of all active names mapped to
// their workspaces.
func ActiveNames() map[string]string {
	result := make(map[string]string)
	activeNames.Range(func(key, value any) bool {
		result[key.(string)] = value.(string)
		return true
	})
	return result
}

// resetForTesting clears all active names. Exported only via test helper.
func resetForTesting() {
	activeNames.Range(func(key, _ any) bool {
		activeNames.Delete(key)
		return true
	})
}
