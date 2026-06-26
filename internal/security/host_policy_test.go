// host_policy_test.go contains tests for host command policy enforcement.
// These tests verify command whitelisting, dangerous mode, and pipe/redirect rejection
// for host OS command execution.
//
// host_policy_test.goはホストコマンドポリシーの適用テストを含みます。
// コマンドホワイトリスト、危険モード、パイプ/リダイレクト拒否の検証を行います。
package security

import (
	"strings"
	"testing"

	"github.com/YujiSuzuki/hostmcp/internal/config"
)

// newTestHostConfig creates a test configuration for host command policy tests.
// It includes basic whitelisted commands (git, df, free), denied commands,
// and dangerous mode with git lifecycle commands enabled.
//
// newTestHostConfigはホストコマンドポリシーテスト用の設定を作成します。
// 基本的なホワイトリストコマンド（git、df、free）、拒否コマンド、
// gitライフサイクルコマンドを有効にした危険モードを含みます。
func newTestHostConfig() *config.HostCommandsConfig {
	return &config.HostCommandsConfig{
		Enabled: true,
		Whitelist: map[string][]string{
			"git": {"status", "diff *", "log --oneline *"},
			"df":  {"-h"},
			"free": {"-m"},
		},
		Deny: map[string][]string{},
		Dangerously: config.HostCommandsDangerously{
			Enabled: true,
			Commands: map[string][]string{
				"git": {"checkout", "pull"},
			},
		},
	}
}

// --- Docker command rejection tests ---

// TestHostCommandPolicy_DockerCommandRejected verifies that docker and docker-compose
// commands are rejected with a descriptive error message directing users to use
// MCP tools or host_tools instead.
//
// TestHostCommandPolicy_DockerCommandRejectedはdockerおよびdocker-composeコマンドが
// MCPツールまたはhost_toolsの使用を促す説明的なエラーメッセージと共に拒否されることを検証します。
func TestHostCommandPolicy_DockerCommandRejected(t *testing.T) {
	p := NewHostCommandPolicy(newTestHostConfig())

	tests := []struct {
		name    string
		command string
	}{
		{"docker ps", "docker ps"},
		{"docker logs", "docker logs mycontainer"},
		{"docker compose up", "docker compose up -d"},
		{"docker-compose up", "docker-compose up"},
		{"docker-compose -p project up", "docker-compose -p myproject up"},
		{"docker restart", "docker restart mycontainer"},
		{"docker build", "docker build ."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := p.CanExecHostCommand(tt.command)
			if ok {
				t.Errorf("CanExecHostCommand(%q) = true, want false", tt.command)
			}
			if err == nil {
				t.Errorf("CanExecHostCommand(%q) should return error", tt.command)
			}
		})
	}
}

// TestHostCommandPolicy_DockerCommandRejectedDangerously verifies that docker commands
// are also rejected in dangerous mode.
//
// TestHostCommandPolicy_DockerCommandRejectedDangerouslyはdockerコマンドが
// 危険モードでも拒否されることを検証します。
func TestHostCommandPolicy_DockerCommandRejectedDangerously(t *testing.T) {
	p := NewHostCommandPolicy(newTestHostConfig())

	tests := []struct {
		name    string
		command string
	}{
		{"docker restart", "docker restart mycontainer"},
		{"docker-compose up", "docker-compose up"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := p.CanExecHostCommandDangerously(tt.command)
			if ok {
				t.Errorf("CanExecHostCommandDangerously(%q) = true, want false", tt.command)
			}
			if err == nil {
				t.Errorf("CanExecHostCommandDangerously(%q) should return error", tt.command)
			}
		})
	}
}

// --- Normal mode tests ---

// TestHostCommandPolicy_NormalMode_Allowed verifies that whitelisted commands
// are accepted in normal mode. Tests git, df, and free commands
// with various arguments matching the whitelist patterns.
//
// TestHostCommandPolicy_NormalMode_Allowedは通常モードでホワイトリストコマンドが
// 許可されることを検証します。ホワイトリストパターンに一致する
// git、df、freeコマンドを様々な引数でテストします。
func TestHostCommandPolicy_NormalMode_Allowed(t *testing.T) {
	p := NewHostCommandPolicy(newTestHostConfig())

	tests := []struct {
		name    string
		command string
	}{
		{"git status", "git status"},
		{"git diff", "git diff HEAD"},
		{"df -h", "df -h"},
		{"free -m", "free -m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := p.CanExecHostCommand(tt.command)
			if err != nil {
				t.Errorf("CanExecHostCommand(%q) error = %v", tt.command, err)
			}
			if !ok {
				t.Errorf("CanExecHostCommand(%q) = false, want true", tt.command)
			}
		})
	}
}

// TestHostCommandPolicy_NormalMode_Denied verifies that non-whitelisted commands
// are rejected in normal mode.
//
// TestHostCommandPolicy_NormalMode_Deniedは通常モードで非ホワイトリストコマンドが
// 拒否されることを検証します。
func TestHostCommandPolicy_NormalMode_Denied(t *testing.T) {
	p := NewHostCommandPolicy(newTestHostConfig())

	tests := []struct {
		name    string
		command string
	}{
		{"not in whitelist", "curl http://localhost"},
		{"unknown command", "ls -la"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := p.CanExecHostCommand(tt.command)
			if ok {
				t.Errorf("CanExecHostCommand(%q) = true, want false", tt.command)
			}
			if err == nil {
				t.Errorf("CanExecHostCommand(%q) should return error", tt.command)
			}
		})
	}
}

// TestHostCommandPolicy_NormalMode_PipeRejected verifies that commands containing
// pipes, redirects, command separators, command substitution, or newlines are rejected
// in normal mode to prevent shell injection attacks.
//
// TestHostCommandPolicy_NormalMode_PipeRejectedは通常モードでパイプ、リダイレクト、
// コマンド区切り、コマンド置換、改行を含むコマンドがシェルインジェクション攻撃を
// 防ぐために拒否されることを検証します。
func TestHostCommandPolicy_NormalMode_PipeRejected(t *testing.T) {
	p := NewHostCommandPolicy(newTestHostConfig())

	tests := []string{
		"git status | grep modified",
		"git log > /tmp/out",
		"git status; rm -rf /",
		"git status && echo hacked",
		"git diff $(cat /etc/passwd)",
		"git diff `cat /etc/passwd`",
		"git diff HEAD\nrm -rf /",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			ok, err := p.CanExecHostCommand(cmd)
			if ok {
				t.Errorf("CanExecHostCommand(%q) should reject pipes/redirects", cmd)
			}
			if err == nil {
				t.Error("should return error")
			}
		})
	}
}

// TestHostCommandPolicy_NormalMode_HintForDangerous verifies that when a dangerous
// command is rejected in normal mode, the error message hints about using dangerous mode.
// This helps users understand that the command is available with the dangerously flag.
//
// TestHostCommandPolicy_NormalMode_HintForDangerousは通常モードで危険なコマンドが
// 拒否された際、エラーメッセージに危険モードの使用をヒントすることを検証します。
func TestHostCommandPolicy_NormalMode_HintForDangerous(t *testing.T) {
	p := NewHostCommandPolicy(newTestHostConfig())

	_, err := p.CanExecHostCommand("git checkout main")
	if err == nil {
		t.Error("should return error")
	}
	if err != nil && !strings.Contains(err.Error(), "dangerously") {
		t.Errorf("error should mention dangerously hint, got: %s", err.Error())
	}
}

// --- Dangerous mode tests ---

// TestHostCommandPolicy_DangerousMode_Allowed verifies that dangerous commands
// (git checkout, pull) and normal whitelisted commands are accepted in dangerous mode.
//
// TestHostCommandPolicy_DangerousMode_Allowedは危険モード使用時に
// 危険コマンド（git checkout、pull）と通常のホワイトリストコマンドが許可されることを検証します。
func TestHostCommandPolicy_DangerousMode_Allowed(t *testing.T) {
	p := NewHostCommandPolicy(newTestHostConfig())

	tests := []struct {
		name    string
		command string
	}{
		{"git checkout", "git checkout main"},
		{"git pull", "git pull origin main"},
		// Normal whitelist commands should also work in dangerous mode
		{"git status via dangerous", "git status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := p.CanExecHostCommandDangerously(tt.command)
			if err != nil {
				t.Errorf("CanExecHostCommandDangerously(%q) error = %v", tt.command, err)
			}
			if !ok {
				t.Errorf("CanExecHostCommandDangerously(%q) = false, want true", tt.command)
			}
		})
	}
}

// TestHostCommandPolicy_DangerousMode_Denied verifies that commands not in either
// the whitelist or dangerous list are still rejected in dangerous mode.
//
// TestHostCommandPolicy_DangerousMode_Deniedはホワイトリストにも危険リストにも
// 無いコマンドが危険モードでも拒否されることを検証します。
func TestHostCommandPolicy_DangerousMode_Denied(t *testing.T) {
	p := NewHostCommandPolicy(newTestHostConfig())

	tests := []struct {
		name    string
		command string
	}{
		{"not in any list", "curl http://localhost"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := p.CanExecHostCommandDangerously(tt.command)
			if ok {
				t.Errorf("CanExecHostCommandDangerously(%q) = true, want false", tt.command)
			}
			if err == nil {
				t.Errorf("CanExecHostCommandDangerously(%q) should return error", tt.command)
			}
		})
	}
}

// TestHostCommandPolicy_DangerousMode_Disabled verifies that dangerous commands
// are rejected when dangerous mode is disabled in the configuration.
//
// TestHostCommandPolicy_DangerousMode_Disabledは設定で危険モードが無効の場合に
// 危険コマンドが拒否されることを検証します。
func TestHostCommandPolicy_DangerousMode_Disabled(t *testing.T) {
	cfg := newTestHostConfig()
	cfg.Dangerously.Enabled = false
	p := NewHostCommandPolicy(cfg)

	_, err := p.CanExecHostCommandDangerously("git checkout main")
	if err == nil {
		t.Error("should return error when dangerous mode is disabled")
	}
}

// TestHostCommandPolicy_DangerousMode_PathTraversal verifies that path traversal
// attempts (../) are rejected even in dangerous mode.
//
// TestHostCommandPolicy_DangerousMode_PathTraversalは危険モードでも
// パストラバーサル（../）が拒否されることを検証します。
func TestHostCommandPolicy_DangerousMode_PathTraversal(t *testing.T) {
	p := NewHostCommandPolicy(newTestHostConfig())

	_, err := p.CanExecHostCommandDangerously("git checkout ../../etc/passwd")
	if err == nil {
		t.Error("should reject path traversal in dangerous mode")
	}
}

// --- Disabled tests ---

// TestHostCommandPolicy_Disabled verifies that all host commands are rejected
// when host command execution is disabled in the configuration.
//
// TestHostCommandPolicy_Disabledは設定でホストコマンド実行が無効の場合に
// すべてのホストコマンドが拒否されることを検証します。
func TestHostCommandPolicy_Disabled(t *testing.T) {
	cfg := newTestHostConfig()
	cfg.Enabled = false
	p := NewHostCommandPolicy(cfg)

	_, err := p.CanExecHostCommand("git status")
	if err == nil {
		t.Error("should return error when host commands are disabled")
	}

	_, err = p.CanExecHostCommandDangerously("git status")
	if err == nil {
		t.Error("should return error when host commands are disabled")
	}
}

// --- GetAllowedHostCommands ---

// TestHostCommandPolicy_GetAllowedHostCommands verifies that GetAllowedHostCommands
// returns the complete whitelist mapping from the configuration.
//
// TestHostCommandPolicy_GetAllowedHostCommandsはGetAllowedHostCommandsが
// 設定からホワイトリストマッピング全体を返すことを検証します。
func TestHostCommandPolicy_GetAllowedHostCommands(t *testing.T) {
	p := NewHostCommandPolicy(newTestHostConfig())
	cmds := p.GetAllowedHostCommands()

	if len(cmds) != 3 {
		t.Errorf("GetAllowedHostCommands() returned %d commands, want 3", len(cmds))
	}

	if _, ok := cmds["git"]; !ok {
		t.Error("should include git commands")
	}
	if _, ok := cmds["df"]; !ok {
		t.Error("should include df commands")
	}
}

// --- Wildcard pattern tests ---

// TestHostCommandPolicy_WildcardPatterns verifies that wildcard patterns (*)
// in the whitelist correctly match arbitrary arguments.
//
// TestHostCommandPolicy_WildcardPatternsはホワイトリストのワイルドカードパターン（*）が
// 任意の引数に正しくマッチすることを検証します。
func TestHostCommandPolicy_WildcardPatterns(t *testing.T) {
	p := NewHostCommandPolicy(newTestHostConfig())

	// "diff *" should match any argument
	ok, err := p.CanExecHostCommand("git diff HEAD~1")
	if err != nil || !ok {
		t.Errorf("should match 'diff *' pattern: ok=%v, err=%v", ok, err)
	}

	// "log --oneline *" should match any additional args
	ok, err = p.CanExecHostCommand("git log --oneline main")
	if err != nil || !ok {
		t.Errorf("should match 'log --oneline *' pattern: ok=%v, err=%v", ok, err)
	}
}
