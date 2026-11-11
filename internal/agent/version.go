package agent

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/godiff/internal/shellenv"
)

// Version is the version an agent's command line says it is, "" when it is
// not installed or does not tell: the settings page that chooses the agent
// shows it, so that a stale one is seen before a review runs on it.
func Version(ctx context.Context, a Agent) string {
	bin, err := shellenv.LookPath(a.Name)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := output(ctx, bin, "", "--version")
	if err != nil {
		return ""
	}
	return versionLine(string(out))
}

// versionLine picks the version out of what `--version` printed: the last
// word of its first line, as "1.2.3" of "claude 1.2.3" or "codex-cli v1.2.3",
// cut at the spaces a longer line adds after it.
func versionLine(out string) string {
	line, _, _ := strings.Cut(out, "\n")
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	v := strings.TrimPrefix(fields[len(fields)-1], "v")
	if v == "" || len(v) > 32 || v[0] < '0' || v[0] > '9' {
		return ""
	}
	for _, r := range v {
		digit := r >= '0' && r <= '9'
		word := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if !digit && !word && !strings.ContainsRune(".-+~", r) {
			return ""
		}
	}
	return v
}
