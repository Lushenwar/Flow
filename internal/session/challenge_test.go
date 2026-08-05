package session

import (
	"strings"
	"testing"
)

func TestChallengeIsLongAndUnique(t *testing.T) {
	a, err := NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Text) != ChallengeLength {
		t.Fatalf("length %d, want %d", len(a.Text), ChallengeLength)
	}

	b, _ := NewChallenge()
	if a.Text == b.Text || a.ID == b.ID {
		t.Fatal("challenges must not repeat, or the second escape is a paste away")
	}
}

func TestChallengeAvoidsAmbiguousCharacters(t *testing.T) {
	// A failure should mean "you did not mean it", never "you could not read it".
	for i := 0; i < 20; i++ {
		c, _ := NewChallenge()
		if strings.ContainsAny(c.Text, "0O1lI") {
			t.Fatalf("ambiguous character in %q", c.Text)
		}
	}
}

func TestChallengeIsCaseSensitive(t *testing.T) {
	c, _ := NewChallenge()

	if !c.Matches(c.ID, c.Text) {
		t.Fatal("exact text must match")
	}
	if c.Matches(c.ID, strings.ToLower(c.Text)) && c.Text != strings.ToLower(c.Text) {
		t.Fatal("case must matter — it is what forces attention over muscle memory")
	}
	if c.Matches(c.ID, c.Text+"x") {
		t.Fatal("trailing characters must not match")
	}
	if c.Matches(c.ID, c.Text[:len(c.Text)-1]) {
		t.Fatal("a truncated answer must not match")
	}
	if c.Matches(c.ID, "") {
		t.Fatal("empty must not match")
	}
}

func TestChallengeIDMustMatchToo(t *testing.T) {
	a, _ := NewChallenge()
	b, _ := NewChallenge()

	// Answering the right text against the wrong challenge id is not an answer.
	if a.Matches(b.ID, a.Text) {
		t.Fatal("id is not checked")
	}
}
