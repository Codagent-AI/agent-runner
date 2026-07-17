package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

func prepareNextCommandPlugin(command CompletionCommand) (string, error) {
	if !command.Valid() {
		return "", fmt.Errorf("invalid completion command")
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache: %w", err)
	}
	digest := sha256.Sum256([]byte(command.ShellCommand()))
	pluginDir := filepath.Join(cacheDir, "agent-runner", "completion-plugins", "next-"+hex.EncodeToString(digest[:6]))
	manifest := `{
  "name": "agent-runner-completion",
  "version": "1.0.0",
  "description": "Agent Runner control-channel completion",
  "commands": "commands/"
}
`
	next := `---
description: Complete the current Agent Runner workflow step
---

Run this exact command now, then finish the response:

` + "`" + command.ShellCommand() + "`" + `
`
	files := map[string]string{
		"plugin.json": manifest,
		filepath.Join(".claude-plugin", "plugin.json"): manifest,
		filepath.Join("commands", "next.md"):           next,
	}
	for relativePath, content := range files {
		path := filepath.Join(pluginDir, relativePath)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", fmt.Errorf("create completion command plugin: %w", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return "", fmt.Errorf("write completion command plugin: %w", err)
		}
	}
	return pluginDir, nil
}
