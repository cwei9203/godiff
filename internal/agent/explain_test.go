package agent

import (
	"strings"
	"testing"
)

// piNotSignedIn is what Pi prints when it is signed in to no model provider,
// verbatim, paths into its install and all.
const piNotSignedIn = `No models available. Use /login to log into a provider via OAuth or API key. See:
/Users/chen_wei/.pi/agent/install/releases/1.5.1/node_modules/@earendil-works/pi-coding-agent/docs/providers.md
/Users/chen_wei/.pi/agent/install/releases/1.5.1/node_modules/@earendil-works/pi-coding-agent/docs/models.md`

func TestExplain(t *testing.T) {
	for _, tc := range []struct {
		what string
		raw  string
		want string
	}{
		{"pi, signed in to no provider", piNotSignedIn, "Pi is not signed in to a model provider."},
		{"pi, in another case", "NO MODELS AVAILABLE", "Pi is not signed in to a model provider."},
		{"a login", "Error: not logged in. Run `codex login` first.", "The agent is not signed in."},
		{"an unauthorized answer", "request failed: 401 Unauthorized", "The agent is not signed in."},
		{"an authentication failure", "Authentication failed: invalid API key", "The agent is not signed in."},
		{"a command that is not there", "fork/exec /usr/local/bin/codex: exec: executable file not found in $PATH", "The agent is not installed"},
		{"a shell that does not know it", "sh: 1: claude: command not found", "The agent is not installed"},
		{"a name that does not resolve", "dial tcp: lookup api.anthropic.com: no such host", "The agent could not reach its service."},
		{"a port that nothing answers on", "dial tcp 127.0.0.1:11434: connect: connection refused", "The agent could not reach its service."},
		{"a network that is gone", "connect: network is unreachable", "The agent could not reach its service."},
		{"a run out of time", "Codex failed: context deadline exceeded", "The agent did not answer in time."},
		{"a request that timed out", "Error: request timed out after 30s", "The agent did not answer in time."},
	} {
		t.Run(tc.what, func(t *testing.T) {
			message, detail := Explain(tc.raw)
			if !strings.Contains(message, tc.want) {
				t.Errorf("message %q, want it to contain %q", message, tc.want)
			}
			// The raw text is never lost: a caller folds it away, or shows
			// it as it is.
			if detail != tc.raw {
				t.Errorf("detail %q, want the raw text %q", detail, tc.raw)
			}
		})
	}
}

func TestExplainUnknown(t *testing.T) {
	for _, raw := range []string{"it answered nothing", "could not read the models of Codex: unexpected end of JSON input", ""} {
		message, detail := Explain(raw)
		if message != "" {
			t.Errorf("%q: message %q, want none: a failure it does not know is not one to guess at", raw, message)
		}
		if detail != raw {
			t.Errorf("%q: detail %q", raw, detail)
		}
	}
}

// TestExplainPiIsReadable is the report this layer answers: what the card
// says of Pi signed in to nothing is an action, not a path into its install.
func TestExplainPiIsReadable(t *testing.T) {
	message, detail := Explain(piNotSignedIn)
	if strings.Contains(message, "node_modules") || strings.Contains(message, "/Users/") {
		t.Errorf("the message carries a path of the install: %q", message)
	}
	if !strings.Contains(message, "`pi`") || !strings.Contains(message, "sign in") {
		t.Errorf("the message does not say to sign in: %q", message)
	}
	if !strings.Contains(detail, "node_modules") {
		t.Errorf("the raw text is not kept: %q", detail)
	}
}
