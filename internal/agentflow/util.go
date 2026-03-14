package agentflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(input string) string {
	value := strings.ToLower(strings.TrimSpace(input))
	value = slugPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "task"
	}
	return value
}

func canonicalTaskKey(input string) string {
	return strings.ToLower(strings.TrimSpace(input))
}

func shortHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func repoID(repoRoot string) string {
	base := slugify(filepath.Base(repoRoot))
	return fmt.Sprintf("%s-%s", base, shortHash(repoRoot)[:8])
}

func taskID(repoRoot, source, key string) string {
	return shortHash(repoRoot, source, key)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func shellJoin(args []string) string {
	if len(args) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	return out
}

func stateRootPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("AGENTFLOW_STATE_HOME")); value != "" {
		return filepath.Clean(value), nil
	}
	if value := strings.TrimSpace(os.Getenv("AGENTFLOW_HOME")); value != "" {
		return filepath.Clean(value), nil
	}
	if value := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); value != "" {
		return filepath.Join(value, "agentflow"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "agentflow"), nil
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func canonicalPath(path string) string {
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return clean
	}
	return resolved
}

func normalizeBaseBranch(baseRef, remote string) string {
	baseRef = strings.TrimSpace(baseRef)
	remote = strings.TrimSpace(remote)
	if baseRef == "" {
		return ""
	}
	if remote != "" {
		prefix := remote + "/"
		if strings.HasPrefix(baseRef, prefix) {
			return strings.TrimPrefix(baseRef, prefix)
		}
		remotePrefix := "refs/remotes/" + remote + "/"
		if strings.HasPrefix(baseRef, remotePrefix) {
			return strings.TrimPrefix(baseRef, remotePrefix)
		}
	}
	if strings.HasPrefix(baseRef, "refs/heads/") {
		return strings.TrimPrefix(baseRef, "refs/heads/")
	}
	return baseRef
}
