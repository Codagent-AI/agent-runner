package scripts_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxRunDryRunShowsSafeDockerInvocation(t *testing.T) {
	artifacts := filepath.Join(t.TempDir(), "artifacts")
	cmd := exec.Command("bash", "./sandbox-run.sh",
		"--dry-run",
		"--no-default-secrets",
		"--artifact-dir", artifacts,
		"--env", "ANTHROPIC_API_KEY",
		"--",
		"echo proof",
	)
	cmd.Env = append(os.Environ(),
		"ANTHROPIC_API_KEY=test-key",
		"SSH_AUTH_SOCK=/tmp/ssh-agent.sock",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox-run dry run failed: %v\n%s", err, output)
	}
	text := string(output)
	for _, want := range []string{
		"docker build",
		"-f docker/dev/Dockerfile",
		"-v " + repoRoot(t) + ":/agent-runner-source:ro",
		"-v " + artifacts + ":/artifacts",
		"-e AGENT_RUNNER_SOURCE_COMMIT",
		"-e ANTHROPIC_API_KEY",
		"--shm-size=1g",
		"echo proof",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "--ipc=host") {
		t.Fatalf("sandbox-run should not share host IPC by default:\n%s", text)
	}
	if strings.Contains(text, "SSH_AUTH_SOCK") {
		t.Fatalf("dry-run output leaked SSH_AUTH_SOCK:\n%s", text)
	}
	if strings.Contains(text, "test-key") {
		t.Fatalf("dry-run output leaked env value:\n%s", text)
	}
}

func TestSandboxRunDryRunPreservesMultiArgCommandBoundaries(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("bash", "./sandbox-run.sh",
		"--dry-run",
		"--no-default-secrets",
		"--artifact-dir", filepath.Join(dir, "artifacts"),
		"--",
		"node",
		"-e",
		"console.log(process.argv[1])",
		"hello world",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox-run dry run failed: %v\n%s", err, output)
	}
	text := string(output)
	for _, want := range []string{`bash -lc`, `exec "$@"`, ` -- node -e`, `hello\ world`} {
		if !strings.Contains(text, want) {
			t.Fatalf("dry-run output missing argv-preserving marker %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `node\ -e\ console.log`) {
		t.Fatalf("dry-run output flattened argv into one shell string:\n%s", text)
	}
}

func TestSandboxRunEnvFilePassesNamesWithoutLeakingValues(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "eval-secrets.env")
	if err := os.WriteFile(envFile, []byte("GITHUB_TOKEN=secret-token\n# comment\nexport ANTHROPIC_API_KEY=another-secret\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	cmd := exec.Command("bash", "./sandbox-run.sh",
		"--dry-run",
		"--no-default-secrets",
		"--artifact-dir", filepath.Join(dir, "artifacts"),
		"--env-file", envFile,
		"--",
		"echo proof",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox-run dry run with env file failed: %v\n%s", err, output)
	}
	text := string(output)
	for _, want := range []string{"-e GITHUB_TOKEN", "-e ANTHROPIC_API_KEY"} {
		if !strings.Contains(text, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"secret-token", "another-secret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("dry-run output leaked secret value %q:\n%s", forbidden, text)
		}
	}
}

func TestSandboxRunLoadsDefaultSandboxSecretsFile(t *testing.T) {
	dir := t.TempDir()
	secretsFile := filepath.Join(dir, "sandbox-secrets.env")
	if err := os.WriteFile(secretsFile, []byte("GITHUB_TOKEN=secret-token\n"), 0o600); err != nil {
		t.Fatalf("write sandbox secrets file: %v", err)
	}

	cmd := exec.Command("bash", "./sandbox-run.sh",
		"--dry-run",
		"--artifact-dir", filepath.Join(dir, "artifacts"),
		"--",
		"echo proof",
	)
	cmd.Env = append(os.Environ(), "SANDBOX_SECRETS_FILE="+secretsFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox-run dry run with default secrets failed: %v\n%s", err, output)
	}
	text := string(output)
	if !strings.Contains(text, "-e GITHUB_TOKEN") {
		t.Fatalf("dry-run output missing GITHUB_TOKEN env pass-through:\n%s", text)
	}
	if strings.Contains(text, "secret-token") {
		t.Fatalf("dry-run output leaked secret value:\n%s", text)
	}
}

func TestSandboxRunCanMountSubscriptionAuthFiles(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	for _, path := range []string{
		filepath.Join(home, ".codex"),
		filepath.Join(home, ".claude"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(home, ".codex", "auth.json"):            "{}\n",
		filepath.Join(home, ".codex", "config.toml"):          "[mcp_servers.node_repl]\ncommand = \"node_repl\"\n",
		filepath.Join(home, ".claude", ".credentials.json"):   "{}\n",
		filepath.Join(home, ".claude", "settings.json"):       "{}\n",
		filepath.Join(home, ".claude", "settings.local.json"): "{}\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	cmd := exec.Command("bash", "./sandbox-run.sh",
		"--dry-run",
		"--no-default-secrets",
		"--artifact-dir", filepath.Join(dir, "artifacts"),
		"--mount-codex-auth",
		"--mount-claude-auth",
		"--",
		"echo proof",
	)
	cmd.Env = append(os.Environ(), "HOME="+home)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox-run dry run with auth mounts failed: %v\n%s", err, output)
	}
	text := string(output)
	for _, want := range []string{
		"target=/host-home/codex/auth.json",
		"target=/host-home/claude/.credentials.json",
		"target=/host-home/claude/settings.json",
		"target=/host-home/claude/settings.local.json",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("dry-run output missing auth mount %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "target=/host-home/codex/config.toml") {
		t.Fatalf("sandbox-run should not mount host Codex config:\n%s", text)
	}
}

func TestSandboxSyncHomeCopiesAllowedFilesWritable(t *testing.T) {
	dir := t.TempDir()
	hostHome := filepath.Join(dir, "host-home")
	containerHome := filepath.Join(dir, "container-home")
	for _, path := range []string{
		filepath.Join(hostHome, "codex"),
		filepath.Join(hostHome, "claude"),
		filepath.Join(hostHome, "shell"),
		filepath.Join(hostHome, "git"),
		filepath.Join(containerHome, ".codex"),
		containerHome,
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	files := map[string]string{
		filepath.Join(hostHome, "codex", "auth.json"):          `{"codex":true}` + "\n",
		filepath.Join(hostHome, "codex", "config.toml"):        "model = \"host-model\"\n[plugins.\"agentmemory@agentmemory\"]\nenabled = true\n[mcp_servers.node_repl]\ncommand = \"node_repl\"\n",
		filepath.Join(hostHome, "claude", ".credentials.json"): `{"claude":true}` + "\n",
		filepath.Join(hostHome, "claude", "settings.json"):     "{}\n",
		filepath.Join(hostHome, "shell", ".zshrc"):             "export TEST_ZSH=1\n",
		filepath.Join(hostHome, "git", ".gitconfig"):           "[user]\n\tname = Test\n[credential]\n\thelper = /opt/homebrew/bin/gh auth git-credential\n",
		filepath.Join(hostHome, "git", "config-ignore"):        "*.tmp\n",
		filepath.Join(hostHome, "sandbox-secrets.env"):         "CLAUDE_CODE_OAUTH_TOKEN=test-oauth-token\nexport GITHUB_TOKEN=test-github-token\n",
	}
	for path, body := range files {
		if err := os.WriteFile(path, []byte(body), 0o400); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	staleCodexConfig := "[marketplaces.agentmemory]\nsource = \"https://example.invalid/agentmemory.git\"\n[mcp_servers.agentmemory]\ncommand = \"npx\"\n"
	if err := os.WriteFile(filepath.Join(containerHome, ".codex", "config.toml"), []byte(staleCodexConfig), 0o600); err != nil {
		t.Fatalf("write stale container codex config: %v", err)
	}

	cmd := exec.Command("bash", "./sandbox-sync-home.sh")
	cmd.Env = append(os.Environ(),
		"HOME="+containerHome,
		"SANDBOX_HOST_HOME_ROOT="+hostHome,
		"SANDBOX_WORKSPACE_BIN="+filepath.Join(dir, "workspace", "bin"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox-sync-home failed: %v\n%s", err, output)
	}

	targets := map[string]string{
		filepath.Join(containerHome, ".codex", "auth.json"):          files[filepath.Join(hostHome, "codex", "auth.json")],
		filepath.Join(containerHome, ".claude", ".credentials.json"): files[filepath.Join(hostHome, "claude", ".credentials.json")],
		filepath.Join(containerHome, ".claude", "settings.json"):     files[filepath.Join(hostHome, "claude", "settings.json")],
		filepath.Join(containerHome, ".zshrc"):                       files[filepath.Join(hostHome, "shell", ".zshrc")],
		filepath.Join(containerHome, ".config", "git", "ignore"):     files[filepath.Join(hostHome, "git", "config-ignore")],
	}
	for path, want := range targets {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read copied file %s: %v", path, err)
		}
		if string(data) != want {
			t.Fatalf("copied file %s = %q, want %q", path, data, want)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat copied file %s: %v", path, err)
		}
		if info.Mode().Perm()&0o200 == 0 {
			t.Fatalf("copied file %s is not writable: %v", path, info.Mode().Perm())
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("copied file %s grants group/other permissions: %v", path, info.Mode().Perm())
		}
	}
	codexConfig := filepath.Join(containerHome, ".codex", "config.toml")
	data, err := os.ReadFile(codexConfig)
	if err != nil {
		t.Fatalf("read codex config: %v", err)
	}
	for _, want := range []string{`# Generated by sandbox-sync-home.sh.`, `[projects."/workspace/agent-runner"]`, `trust_level = "trusted"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("codex config missing %q:\n%s", want, data)
		}
	}
	for _, forbidden := range []string{"host-model", "mcp_servers", "agentmemory", "marketplaces", "plugins"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("codex config should not include host/stale config marker %q:\n%s", forbidden, data)
		}
	}
	sandboxEnv := filepath.Join(containerHome, ".sandbox-env")
	data, err = os.ReadFile(sandboxEnv)
	if err != nil {
		t.Fatalf("read sandbox env: %v", err)
	}
	text := string(data)
	for _, want := range []string{"export CLAUDE_CODE_OAUTH_TOKEN=", "export GITHUB_TOKEN="} {
		if !strings.Contains(text, want) {
			t.Fatalf("sandbox env missing %q:\n%s", want, text)
		}
	}
	info, err := os.Stat(sandboxEnv)
	if err != nil {
		t.Fatalf("stat sandbox env: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("sandbox env grants group/other permissions: %v", info.Mode().Perm())
	}
	zshenv, err := os.ReadFile(filepath.Join(containerHome, ".zshenv"))
	if err != nil {
		t.Fatalf("read zshenv: %v", err)
	}
	if !strings.Contains(string(zshenv), ".sandbox-env") {
		t.Fatalf("zshenv should source sandbox env:\n%s", zshenv)
	}
	knownHosts, err := os.ReadFile(filepath.Join(containerHome, ".ssh", "known_hosts"))
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	for _, want := range []string{"github.com ssh-ed25519", "github.com ecdsa-sha2-nistp256", "github.com ssh-rsa"} {
		if !strings.Contains(string(knownHosts), want) {
			t.Fatalf("known_hosts missing %q:\n%s", want, knownHosts)
		}
	}
	gitConfig, err := os.ReadFile(filepath.Join(containerHome, ".gitconfig"))
	if err != nil {
		t.Fatalf("read git config after sync: %v", err)
	}
	for _, want := range []string{
		"[user]\n\tname = Test",
		`[url "https://github.com/"]`,
		"insteadOf = git@github.com:",
		"insteadOf = ssh://git@github.com/",
		`[credential "https://github.com"]`,
		"helper = !",
		"github-credential-helper",
	} {
		if !strings.Contains(string(gitConfig), want) {
			t.Fatalf("git config missing GitHub HTTPS rewrite %q:\n%s", want, gitConfig)
		}
	}
	if strings.Contains(string(gitConfig), "/opt/homebrew/bin/gh") {
		t.Fatalf("git config kept host-only credential helper:\n%s", gitConfig)
	}
	githubCredentialHelper := filepath.Join(dir, "workspace", "bin", "github-credential-helper")
	helperCmd := exec.Command(githubCredentialHelper, "get")
	helperCmd.Env = append(os.Environ(), "HOME="+containerHome)
	helperCmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	helperOutput, err := helperCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("github credential helper failed: %v\n%s", err, helperOutput)
	}
	if got := string(helperOutput); !strings.Contains(got, "username=x-access-token") || !strings.Contains(got, "password=test-github-token") {
		t.Fatalf("github credential helper did not return sandbox token credentials:\n%s", helperOutput)
	}
	fakeAgentCLI := filepath.Join(dir, "fake-agent-cli")
	if err := os.WriteFile(fakeAgentCLI, []byte("#!/usr/bin/env bash\nprintf 'arg=%s\\n' \"$@\"\nprintf 'github=%s\\n' \"${GITHUB_TOKEN:-}\"\n"), 0o755); err != nil {
		t.Fatalf("write fake agent cli: %v", err)
	}
	wrapperCases := []struct {
		name    string
		path    string
		envName string
		flag    string
	}{
		{
			name:    "codex",
			path:    filepath.Join(dir, "workspace", "bin", "codex"),
			envName: "SANDBOX_REAL_CODEX",
			flag:    "--dangerously-bypass-approvals-and-sandbox",
		},
		{
			name:    "claude",
			path:    filepath.Join(dir, "workspace", "bin", "claude"),
			envName: "SANDBOX_REAL_CLAUDE",
			flag:    "--dangerously-skip-permissions",
		},
	}
	for _, tc := range wrapperCases {
		wrapperCmd := exec.Command(tc.path, "plugin", "list")
		wrapperCmd.Env = append(os.Environ(),
			"HOME="+containerHome,
			tc.envName+"="+fakeAgentCLI,
		)
		wrapperOutput, err := wrapperCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s yolo wrapper failed: %v\n%s", tc.name, err, wrapperOutput)
		}
		got := string(wrapperOutput)
		for _, want := range []string{"arg=" + tc.flag, "arg=plugin", "arg=list", "github=test-github-token"} {
			if !strings.Contains(got, want) {
				t.Fatalf("%s yolo wrapper missing %q:\n%s", tc.name, want, wrapperOutput)
			}
		}
	}

	localConfig := "model = \"local-test\"\n[mcp_servers.node_repl]\ncommand = \"node_repl\"\n\n[projects.\"/workspace/agent-runner\"]\ntrust_level = \"trusted\"\nlocal = true\n"
	if err := os.WriteFile(codexConfig, []byte(localConfig), 0o600); err != nil {
		t.Fatalf("write local codex config: %v", err)
	}
	cmd = exec.Command("bash", "./sandbox-sync-home.sh")
	cmd.Env = append(os.Environ(),
		"HOME="+containerHome,
		"SANDBOX_HOST_HOME_ROOT="+hostHome,
		"SANDBOX_WORKSPACE_BIN="+filepath.Join(dir, "workspace", "bin"),
	)
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox-sync-home second run failed: %v\n%s", err, output)
	}
	data, err = os.ReadFile(codexConfig)
	if err != nil {
		t.Fatalf("read local codex config after second sync: %v", err)
	}
	for _, forbidden := range []string{"local-test", "mcp_servers", "local = true"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("second sync kept local/stale Codex config marker %q:\n%s", forbidden, data)
		}
	}
	for _, want := range []string{`# Generated by sandbox-sync-home.sh.`, `[projects."/workspace/agent-runner"]`, `trust_level = "trusted"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("second sync codex config missing %q:\n%s", want, data)
		}
	}
	gitConfig, err = os.ReadFile(filepath.Join(containerHome, ".gitconfig"))
	if err != nil {
		t.Fatalf("read git config after second sync: %v", err)
	}
	for _, value := range []string{"insteadOf = git@github.com:", "insteadOf = ssh://git@github.com/"} {
		if count := strings.Count(string(gitConfig), value); count != 1 {
			t.Fatalf("second sync wrote %q %d times, want once:\n%s", value, count, gitConfig)
		}
	}
	helper, err := os.ReadFile(filepath.Join(dir, "workspace", "bin", "claude-headless"))
	if err != nil {
		t.Fatalf("read claude-headless helper: %v", err)
	}
	if !strings.Contains(string(helper), "exec claude -p") || !strings.Contains(string(helper), ".sandbox-env") {
		t.Fatalf("claude-headless helper should source sandbox env and run claude -p:\n%s", helper)
	}
}

func TestEvalAndSceneProofDryRunTargetsFixtureAndReference(t *testing.T) {
	dir := t.TempDir()
	artifacts := filepath.Join(dir, "and-scene-proof")
	home := filepath.Join(dir, "home")
	for _, path := range []string{
		filepath.Join(home, ".codex"),
		filepath.Join(home, ".claude"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(home, ".codex", "auth.json"):          "{}\n",
		filepath.Join(home, ".claude", ".credentials.json"): "{}\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	cmd := exec.Command("bash", "./eval-and-scene.sh",
		"--dry-run",
		"--proof-browser",
		"--artifact-dir", artifacts,
		"--mount-codex-auth",
		"--mount-claude-auth",
	)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"SANDBOX_SECRETS_FILE="+filepath.Join(dir, "missing.env"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("eval-and-scene proof dry run failed: %v\n%s", err, output)
	}
	text := string(output)
	for _, want := range []string{
		"origin/eval/create-and-scene-spec-only",
		"origin/change/create-and-scene",
		"npm ci",
		"npm run build",
		"npm run verify",
		"proof-metadata.json",
		"target=/host-home/codex/auth.json",
		"target=/host-home/claude/.credentials.json",
		artifacts,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, text)
		}
	}
	for _, want := range []string{`case "$1" in`, `${GITHUB_TOKEN:-${GH_TOKEN:-}}`} {
		if !strings.Contains(text, want) {
			t.Fatalf("dry-run output missing askpass content %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "https://x-access-token") || strings.Contains(text, "@github.com") {
		t.Fatalf("dry-run output should not include token-in-URL auth wiring:\n%s", text)
	}
	for _, forbidden := range []string{`case "\$1" in`, `\${GITHUB_TOKEN`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("dry-run output contains escaped askpass content %q:\n%s", forbidden, text)
		}
	}
}

func TestEvalAndSceneProofDryRunShellQuotesRepoInputs(t *testing.T) {
	dir := t.TempDir()
	maliciousRepo := `https://example.invalid/repo.git"; echo pwned; #`
	maliciousRef := `origin/eval"; echo ref-pwned; #`

	cmd := exec.Command("bash", "./eval-and-scene.sh",
		"--dry-run",
		"--proof-browser",
		"--artifact-dir", filepath.Join(dir, "artifacts"),
		"--repo", maliciousRepo,
		"--fixture-ref", maliciousRef,
		"--reference-ref", maliciousRef,
	)
	cmd.Env = append(os.Environ(), "SANDBOX_SECRETS_FILE="+filepath.Join(dir, "missing.env"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("eval-and-scene proof dry run failed: %v\n%s", err, output)
	}
	text := string(output)
	for _, forbidden := range []string{`git clone "https://example.invalid/repo.git"; echo pwned; #`, `git checkout "origin/eval"; echo ref-pwned; #`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("dry-run output embedded unquoted shell input %q:\n%s", forbidden, text)
		}
	}
	for _, want := range []string{`repo.git\\"\\;\\ echo\\ pwned`, `origin/eval\\"\\;\\ echo\\ ref-pwned`} {
		if !strings.Contains(text, want) {
			t.Fatalf("dry-run output missing shell-quoted input marker %q:\n%s", want, text)
		}
	}
}

func TestEvalDevcontainerUsesSharedDockerfile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".devcontainer", "eval", "devcontainer.json"))
	if err != nil {
		t.Fatalf("read eval devcontainer: %v", err)
	}
	var doc struct {
		Build struct {
			Dockerfile string `json:"dockerfile"`
			Context    string `json:"context"`
		} `json:"build"`
		RemoteUser        string            `json:"remoteUser"`
		WorkspaceFolder   string            `json:"workspaceFolder"`
		Mounts            []string          `json:"mounts"`
		ContainerEnv      map[string]string `json:"containerEnv"`
		PostCreateCommand string            `json:"postCreateCommand"`
		Customizations    struct {
			VSCode struct {
				Settings map[string]any `json:"settings"`
			} `json:"vscode"`
		} `json:"customizations"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse devcontainer json: %v", err)
	}
	if doc.Build.Dockerfile != "../../docker/dev/Dockerfile" {
		t.Fatalf("dockerfile = %q, want ../../docker/dev/Dockerfile", doc.Build.Dockerfile)
	}
	if doc.Build.Context != "../.." {
		t.Fatalf("context = %q, want ../..", doc.Build.Context)
	}
	if doc.RemoteUser == "" || doc.RemoteUser == "root" {
		t.Fatalf("remoteUser = %q, want a non-root user", doc.RemoteUser)
	}
	if doc.WorkspaceFolder != "/workspace/agent-runner" {
		t.Fatalf("workspaceFolder = %q, want /workspace/agent-runner", doc.WorkspaceFolder)
	}
	for _, want := range []string{
		"target=/workspace/agent-runner",
		"target=/workspace/home,type=volume",
		"target=/artifacts",
	} {
		if !sliceContainsSubstring(doc.Mounts, want) {
			t.Fatalf("devcontainer mounts missing %q: %#v", want, doc.Mounts)
		}
	}
	for _, forbidden := range []string{
		"target=/host-home/codex/auth.json",
		"target=/host-home/codex/config.toml",
		"target=/host-home/claude/.credentials.json",
		"target=/host-home/claude/settings.json",
		"target=/host-home/claude/settings.local.json",
		"target=/host-home/shell/.zshrc",
		"target=/host-home/shell/.zprofile",
		"target=/host-home/git/.gitconfig",
		"target=/host-home/git/.gitignore",
		"target=/host-home/git/config-ignore",
		"target=/host-home/sandbox-secrets.env",
	} {
		if sliceContainsSubstring(doc.Mounts, forbidden) {
			t.Fatalf("devcontainer should not mount host config by default %q: %#v", forbidden, doc.Mounts)
		}
	}
	if got := doc.ContainerEnv["SHELL"]; got != "/usr/bin/zsh" {
		t.Fatalf("containerEnv SHELL = %q, want /usr/bin/zsh", got)
	}
	if got := doc.Customizations.VSCode.Settings["terminal.integrated.defaultProfile.linux"]; got != "zsh" {
		t.Fatalf("default terminal profile = %#v, want zsh", got)
	}
	if !strings.Contains(doc.PostCreateCommand, "scripts/sandbox-sync-home.sh") || !strings.Contains(doc.PostCreateCommand, "make build") {
		t.Fatalf("postCreateCommand = %q, want home sync and make build", doc.PostCreateCommand)
	}
}

func TestDevcontainerShellDefaultsToZsh(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "devcontainer-shell.sh"))
	if err != nil {
		t.Fatalf("read devcontainer-shell.sh: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "exec zsh -l") {
		t.Fatalf("devcontainer-shell.sh should default to a zsh login shell:\n%s", text)
	}
	if !strings.Contains(text, "set -- zsh -lc") {
		t.Fatalf("devcontainer-shell.sh should run commands through zsh:\n%s", text)
	}
	if strings.Count(text, "scripts/sandbox-sync-home.sh") < 2 {
		t.Fatalf("devcontainer-shell.sh should sync home before interactive and one-shot commands:\n%s", text)
	}
	if !strings.Contains(text, `source \"\$HOME/.sandbox-env\"`) {
		t.Fatalf("devcontainer-shell.sh should source sandbox env before one-shot commands:\n%s", text)
	}
	if !strings.Contains(text, ".sandbox-secrets.env") || !strings.Contains(text, "chmod 600") {
		t.Fatalf("devcontainer-shell.sh should create a private sandbox secrets file when missing:\n%s", text)
	}
	for _, want := range []string{
		"--with-host-config",
		"with-host-auth/devcontainer.json",
		"agent-runner-dev-home-host-auth",
		"path.relative(outConfigDir",
		"optionalHostMounts.filter(([source]) => fs.existsSync(source))",
		"requiredHostMounts",
		"target=/host-home/codex/auth.json",
		"target=/host-home/claude/.credentials.json",
		"target=/host-home/sandbox-secrets.env",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("devcontainer-shell.sh missing opt-in host config support %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "target=/host-home/codex/config.toml") {
		t.Fatalf("devcontainer-shell.sh should not mount host Codex config:\n%s", text)
	}
}

func TestLocalEvalSecretsAndArtifactsAreGitignored(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	text := string(data)
	for _, want := range []string{".sandbox-secrets.env", ".eval-secrets.env", "artifacts/"} {
		if !strings.Contains(text, want) {
			t.Fatalf(".gitignore missing %q:\n%s", want, text)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func sliceContainsSubstring(items []string, want string) bool {
	for _, item := range items {
		if strings.Contains(item, want) {
			return true
		}
	}
	return false
}
