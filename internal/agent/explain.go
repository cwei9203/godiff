package agent

import "strings"

// Reason is why an agent's command failed, as godiff can tell it apart from
// the other failures. It is a code rather than a sentence: the interface says
// it in the reader's language, and the agent package names no words of its own.
type Reason uint8

const (
	// ReasonUnknown is a failure godiff does not know; the raw text is all
	// there is to show.
	ReasonUnknown Reason = iota
	// ReasonNoModels is Pi naming no model at all, signed in to nothing.
	ReasonNoModels
	// ReasonNotSignedIn is an agent whose provider refuses it as unknown.
	ReasonNotSignedIn
	// ReasonNotInstalled is a command that is not there, or not on PATH.
	ReasonNotInstalled
	// ReasonNetwork is an agent that could not reach its service.
	ReasonNetwork
	// ReasonTimeout is an agent that did not answer in time.
	ReasonTimeout
	// ReasonNotVerified is an agent godiff does not know how to let write
	// files: the way has not been verified, rather than the agent being
	// unable to.
	ReasonNotVerified
)

// String names a reason, for a log or a test.
func (r Reason) String() string {
	switch r {
	case ReasonNoModels:
		return "no models"
	case ReasonNotSignedIn:
		return "not signed in"
	case ReasonNotInstalled:
		return "not installed"
	case ReasonNetwork:
		return "network"
	case ReasonTimeout:
		return "timeout"
	case ReasonNotVerified:
		return "not verified"
	}
	return "unknown"
}

// explain is a failure an agent's command line gives, as the fragments of the
// raw text that tell it apart from the others, and the reason it is.
type explain struct {
	// clues are fragments of the failure, in lower case, any one of which
	// tells it: agents word the same failure their own way.
	clues  []string
	reason Reason
}

// explains are the failures godiff knows, in the order they are looked for:
// Pi's own answer first, as its words for a missing login are not the
// others', and the most general last.
var explains = []explain{
	{
		// Pi names no models either when no provider is signed in or when
		// its agent directory holds none, as a PI_CODING_AGENT_DIR of
		// another home does.
		clues:  []string{"no models available", "log into a provider"},
		reason: ReasonNoModels,
	},
	{
		clues:  []string{"not logged in", "unauthorized", "401", "authentication"},
		reason: ReasonNotSignedIn,
	},
	{
		clues:  []string{"executable file not found", "command not found"},
		reason: ReasonNotInstalled,
	},
	{
		clues:  []string{"no such host", "connection refused", "network is unreachable"},
		reason: ReasonNetwork,
	},
	{
		clues:  []string{"context deadline exceeded", "timed out"},
		reason: ReasonTimeout,
	},
}

// Explain tells why a CLI's failure happened, and keeps the raw text: the
// reason is ReasonUnknown when the failure is none it knows, and then the raw
// text is all there is to show.
func Explain(raw string) (Reason, string) {
	text := strings.ToLower(raw)
	for _, e := range explains {
		for _, clue := range e.clues {
			if strings.Contains(text, clue) {
				return e.reason, raw
			}
		}
	}
	return ReasonUnknown, raw
}
