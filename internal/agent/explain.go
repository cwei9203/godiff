package agent

import "strings"

// explain is a failure an agent's command line gives, as a person reads it:
// the fragments of the raw text that tell it apart from the others, and what
// to say about it.
type explain struct {
	// clues are fragments of the failure, in lower case, any one of which
	// tells it: agents word the same failure their own way.
	clues   []string
	message string
}

// explains are the failures godiff knows, in the order they are looked for:
// Pi's own answer first, as its words for a missing login are not the
// others', and the most general last.
var explains = []explain{
	{
		clues: []string{"no models available", "log into a provider"},
		message: "Pi is not signed in to a model provider. " +
			"Run pi in a terminal and sign in, then try again.",
	},
	{
		clues: []string{"not logged in", "unauthorized", "401", "authentication"},
		message: "The agent is not signed in. " +
			"Sign in with its own CLI in a terminal, then try again.",
	},
	{
		clues: []string{"executable file not found", "command not found"},
		message: "The agent is not installed, or not on PATH. " +
			"Install it, or choose another agent in Settings.",
	},
	{
		clues: []string{"no such host", "connection refused", "network is unreachable"},
		message: "The agent could not reach its service. " +
			"Check the network, then try again.",
	},
	{
		clues: []string{"context deadline exceeded", "timed out"},
		message: "The agent did not answer in time. " +
			"Try again, or choose a faster model.",
	},
}

// Explain turns a CLI's raw failure into what a person can act on: what went
// wrong, and what to do about it. The detail is always the raw text, for a
// caller that sets it aside; the message is "" when the failure is none it
// knows, and then the raw text is all there is to show.
func Explain(raw string) (message, detail string) {
	text := strings.ToLower(raw)
	for _, e := range explains {
		for _, clue := range e.clues {
			if strings.Contains(text, clue) {
				return e.message, raw
			}
		}
	}
	return "", raw
}
