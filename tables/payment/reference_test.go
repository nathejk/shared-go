package payment

import (
	"strings"
	"testing"
)

func TestNewReferenceFormat(t *testing.T) {
	ref, err := newReference()
	if err != nil {
		t.Fatalf("newReference: %v", err)
	}
	if len(ref) != referenceLength {
		t.Errorf("length = %d, want %d (%q)", len(ref), referenceLength, ref)
	}
	// MobilePay requires at least 8 characters; keep margin above it rather
	// than sitting exactly on the limit.
	if len(ref) <= 8 {
		t.Errorf("reference %q leaves no margin over the provider's 8-character minimum", ref)
	}
}

// The alphabet is Crockford base32 precisely so a reference read off the portal,
// pasted into a ticket or spelled out over the phone cannot come back wrong.
func TestNewReferenceAvoidsAmbiguousCharacters(t *testing.T) {
	// 2000 draws is 24000 characters; every alphabet slot is hit many times
	// over, so a stray character would show up reliably.
	for range 2000 {
		ref, err := newReference()
		if err != nil {
			t.Fatalf("newReference: %v", err)
		}
		for _, c := range ref {
			if strings.ContainsRune("ILOU", c) {
				t.Fatalf("%q contains %q, which is confusable with 1/0 or spells badly", ref, c)
			}
			if !strings.ContainsRune(referenceAlphabet, c) {
				t.Fatalf("%q contains %q, outside the alphabet", ref, c)
			}
		}
		if ref != strings.ToUpper(ref) {
			t.Fatalf("%q should be uppercase so it survives being typed by hand", ref)
		}
	}
}

// Not a proof of uniformity, but it catches the obvious ways a generator goes
// wrong: an unreachable part of the alphabet, or a character appearing far too
// often.
func TestNewReferenceUsesTheWholeAlphabet(t *testing.T) {
	seen := map[rune]int{}
	const draws = 3000
	for range draws {
		ref, _ := newReference()
		for _, c := range ref {
			seen[c]++
		}
	}
	if len(seen) != len(referenceAlphabet) {
		var missing []string
		for _, c := range referenceAlphabet {
			if seen[c] == 0 {
				missing = append(missing, string(c))
			}
		}
		t.Errorf("only %d of %d characters ever appeared; missing %v",
			len(seen), len(referenceAlphabet), missing)
	}
	expected := draws * referenceLength / len(referenceAlphabet)
	for c, n := range seen {
		if n < expected/2 || n > expected*2 {
			t.Errorf("character %q appeared %d times, expected around %d", string(c), n, expected)
		}
	}
}

func TestNewReferenceIsUnpredictable(t *testing.T) {
	seen := map[string]bool{}
	for range 2000 {
		ref, _ := newReference()
		if seen[ref] {
			t.Fatalf("repeated reference %q in 2000 draws", ref)
		}
		seen[ref] = true
	}
}
