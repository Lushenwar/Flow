package session

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
)

// ChallengeLength is how much you type to end a session early. Long enough to be
// deliberate, short enough that it is not a punishment — the delay is the
// friction, this is only proof that you meant it.
const ChallengeLength = 100

var ErrChallenge = errors.New("challenge_mismatch")

// challengeAlphabet excludes the characters people cannot tell apart in a UI
// font: 0/O, 1/l/I. A failure here should mean "you did not mean it", never
// "you could not read it".
const challengeAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

type Challenge struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func NewChallenge() (Challenge, error) {
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return Challenge{}, err
	}
	buf := make([]byte, ChallengeLength)
	if _, err := rand.Read(buf); err != nil {
		return Challenge{}, err
	}
	text := make([]byte, ChallengeLength)
	for i, b := range buf {
		text[i] = challengeAlphabet[int(b)%len(challengeAlphabet)]
	}
	return Challenge{ID: hex.EncodeToString(id), Text: string(text)}, nil
}

// Matches is case-sensitive and constant-time. Case sensitivity is the point:
// it forces attention rather than muscle memory.
func (c Challenge) Matches(id, typed string) bool {
	if subtle.ConstantTimeCompare([]byte(c.ID), []byte(id)) != 1 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Text), []byte(typed)) == 1
}
