package names

import (
	"sync"
	"testing"
)

func TestGenerateReturnsNonEmpty(t *testing.T) {
	resetForTesting()

	name := Generate("workspace-a")
	if name == "" {
		t.Fatal("Generate returned an empty string")
	}
}

func TestGenerateReturnsUniqueNamesForSameWorkspace(t *testing.T) {
	resetForTesting()

	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		name := Generate("workspace-b")
		if seen[name] {
			t.Fatalf("duplicate name %q on iteration %d", name, i)
		}
		seen[name] = true
	}
}

func TestReleaseFreesName(t *testing.T) {
	resetForTesting()

	name := Generate("workspace-c")
	if !IsActive(name) {
		t.Fatalf("expected %q to be active after Generate", name)
	}

	Release(name)
	if IsActive(name) {
		t.Fatalf("expected %q to be inactive after Release", name)
	}

	// The released name should be eligible for reuse.
	// Generate all names except one slot that was freed.
	resetForTesting()
	// Claim every name in the word list except leave room for one.
	for i := 0; i < len(wordList)-1; i++ {
		Generate("workspace-c")
	}

	// Release one active name.
	active := ActiveNames()
	var releasedName string
	for n := range active {
		releasedName = n
		break
	}
	Release(releasedName)

	// Next Generate should be able to return a base name (no number suffix).
	got := Generate("workspace-c")
	// It should be one of the base words, not a numbered variant.
	isBase := false
	for _, w := range wordList {
		if got == w {
			isBase = true
			break
		}
	}
	if !isBase {
		t.Fatalf("expected a base word after Release, got %q", got)
	}
}

func TestExhaustionAppendsNumbers(t *testing.T) {
	resetForTesting()

	// Claim every base name.
	for i := 0; i < len(wordList); i++ {
		name := Generate("workspace-d")
		if name == "" {
			t.Fatalf("Generate returned empty on iteration %d", i)
		}
	}

	// All base names should now be active.
	active := ActiveNames()
	if len(active) != len(wordList) {
		t.Fatalf("expected %d active names, got %d", len(wordList), len(active))
	}

	// Next Generate must produce a numbered variant.
	numbered := Generate("workspace-d")
	if numbered == "" {
		t.Fatal("Generate returned empty after exhaustion")
	}

	// Verify the numbered name is not a bare base word.
	for _, w := range wordList {
		if numbered == w {
			t.Fatalf("expected a numbered name, got bare base word %q", numbered)
		}
	}

	// It should end with a digit.
	last := numbered[len(numbered)-1]
	if last < '0' || last > '9' {
		t.Fatalf("expected numbered suffix, got %q", numbered)
	}
}

func TestActiveNamesReturnsSnapshot(t *testing.T) {
	resetForTesting()

	Generate("ws1")
	Generate("ws2")
	Generate("ws3")

	active := ActiveNames()
	if len(active) != 3 {
		t.Fatalf("expected 3 active names, got %d", len(active))
	}

	// Modifying the returned map should not affect internal state.
	for k := range active {
		delete(active, k)
	}
	if len(ActiveNames()) != 3 {
		t.Fatal("deleting from returned map affected internal state")
	}
}

func TestConcurrentGenerationIsSafe(t *testing.T) {
	resetForTesting()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	results := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			name := Generate("concurrent-ws")
			results <- name
		}(i)
	}

	wg.Wait()
	close(results)

	seen := make(map[string]bool)
	for name := range results {
		if name == "" {
			t.Fatal("concurrent Generate returned empty string")
		}
		if seen[name] {
			t.Fatalf("concurrent Generate produced duplicate: %q", name)
		}
		seen[name] = true
	}

	if len(seen) != goroutines {
		t.Fatalf("expected %d unique names, got %d", goroutines, len(seen))
	}
}

func TestWordListHasMinimumSize(t *testing.T) {
	if len(wordList) < 50 {
		t.Fatalf("wordList should have at least 50 entries, has %d", len(wordList))
	}
}
