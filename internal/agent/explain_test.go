package agent

import "testing"

// piNotSignedIn is what Pi prints when it is signed in to no model provider,
// verbatim, paths into its install and all.
const piNotSignedIn = `No models available. Use /login to log into a provider via OAuth or API key. See:
/Users/chen_wei/.pi/agent/install/releases/1.5.1/node_modules/@earendil-works/pi-coding-agent/docs/providers.md
/Users/chen_wei/.pi/agent/install/releases/1.5.1/node_modules/@earendil-works/pi-coding-agent/docs/models.md`

func TestExplain(t *testing.T) {
	for _, tc := range []struct {
		what string
		raw  string
		want Reason
	}{
		{"pi, signed in to no provider", piNotSignedIn, ReasonNoModels},
		{"pi, in another case", "NO MODELS AVAILABLE", ReasonNoModels},
		{"a login", "Error: not logged in. Run `codex login` first.", ReasonNotSignedIn},
		{"an unauthorized answer", "request failed: 401 Unauthorized", ReasonNotSignedIn},
		{"an authentication failure", "Authentication failed: invalid API key", ReasonNotSignedIn},
		{"a command that is not there", "fork/exec /usr/local/bin/codex: exec: executable file not found in $PATH", ReasonNotInstalled},
		{"a shell that does not know it", "sh: 1: claude: command not found", ReasonNotInstalled},
		{"a name that does not resolve", "dial tcp: lookup api.anthropic.com: no such host", ReasonNetwork},
		{"a port that nothing answers on", "dial tcp 127.0.0.1:11434: connect: connection refused", ReasonNetwork},
		{"a network that is gone", "connect: network is unreachable", ReasonNetwork},
		{"a run out of time", "Codex failed: context deadline exceeded", ReasonTimeout},
		{"a request that timed out", "Error: request timed out after 30s", ReasonTimeout},
	} {
		t.Run(tc.what, func(t *testing.T) {
			reason, detail := Explain(tc.raw)
			if reason != tc.want {
				t.Errorf("reason %v, want %v", reason, tc.want)
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
		reason, detail := Explain(raw)
		if reason != ReasonUnknown {
			t.Errorf("%q: reason %v, want ReasonUnknown: a failure it does not know is not one to guess at", raw, reason)
		}
		if detail != raw {
			t.Errorf("%q: detail %q", raw, detail)
		}
	}
}

// TestExplainKeepsTheRawText is the report this layer answers: the reason is
// Pi's own, and the raw text, paths into its install and all, is kept for a
// caller that shows it folded away.
func TestExplainKeepsTheRawText(t *testing.T) {
	reason, detail := Explain(piNotSignedIn)
	if reason != ReasonNoModels {
		t.Errorf("reason %v, want ReasonNoModels", reason)
	}
	if detail != piNotSignedIn {
		t.Errorf("the raw text is not kept: %q", detail)
	}
}
