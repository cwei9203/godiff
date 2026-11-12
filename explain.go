package main

import (
	"strings"

	"github.com/egoist/godiff/internal/agent"
)

// textReason says why an agent's command failed, in the language of the
// interface. It is "" for a failure godiff does not know, whose own text is
// then all there is to show.
func textReason(reason agent.Reason) string {
	switch reason {
	case agent.ReasonNoModels:
		return msg().ExplainNoModels()
	case agent.ReasonNotSignedIn:
		return msg().ExplainNotSignedIn()
	case agent.ReasonNotInstalled:
		return msg().ExplainNotInstalled()
	case agent.ReasonNetwork:
		return msg().ExplainNetwork()
	case agent.ReasonTimeout:
		return msg().ExplainTimeout()
	case agent.ReasonNotVerified:
		return msg().ExplainNotVerified()
	}
	return ""
}

// explainFailure is what an agent's raw failure says to a person: the reason
// in the interface's words, and the raw text kept under it.
func explainFailure(raw string) (message, detail string) {
	reason, detail := agent.Explain(raw)
	return textReason(reason), detail
}

// explainText is explainFailure for a caller that always shows a sentence: an
// unknown failure says its own raw text, or the tail of it.
func explainText(reason agent.Reason, detail string) string {
	if message := textReason(reason); message != "" {
		return message
	}
	return strings.TrimSpace(detail)
}
