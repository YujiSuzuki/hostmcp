// Package security provides the HostCommandPolicy for host CLI command execution.
// It enforces whitelist-based access control with deny lists and dangerous mode.
//
// securityパッケージはホストCLIコマンド実行のためのHostCommandPolicyを提供します。
// ホワイトリストベースのアクセス制御と拒否リストおよび危険モードを適用します。
package security

import (
	"fmt"
	"strings"

	"github.com/YujiSuzuki/hostmcp/internal/config"
)

// HostCommandPolicy enforces security rules for host command execution.
// HostCommandPolicyはホストコマンド実行のセキュリティルールを適用します。
type HostCommandPolicy struct {
	config *config.HostCommandsConfig
}

// NewHostCommandPolicy creates a new HostCommandPolicy from config.
// NewHostCommandPolicyは設定から新しいHostCommandPolicyを作成します。
func NewHostCommandPolicy(cfg *config.HostCommandsConfig) *HostCommandPolicy {
	return &HostCommandPolicy{config: cfg}
}

// CanExecHostCommand checks if a command is allowed in normal mode.
// Checks: docker rejection + whitelist match + deny check + pipe/redirect check.
//
// CanExecHostCommandは通常モードでコマンドが許可されているかチェックします。
// チェック: docker拒否 + ホワイトリストマッチ + 拒否チェック + パイプ/リダイレクトチェック
func (p *HostCommandPolicy) CanExecHostCommand(command string) (bool, error) {
	if !p.config.Enabled {
		return false, fmt.Errorf("host commands are disabled")
	}

	// Check for pipes and redirects
	// パイプとリダイレクトのチェック
	if containsShellMetaChars(command) {
		return false, fmt.Errorf("shell meta-characters are not allowed: %s", command)
	}

	// Parse the command
	// コマンドを解析
	baseCmd, args, err := splitCommand(command)
	if err != nil {
		return false, fmt.Errorf("failed to parse command: %w", err)
	}

	// Reject docker/docker-compose commands.
	// For monitoring use MCP tools (get_logs, get_stats, inspect_container, etc.).
	// For lifecycle operations use host_tools (.sandbox/host-tools/).
	//
	// docker/docker-composeコマンドを拒否します。
	// 監視にはMCPツール（get_logs、get_stats、inspect_container等）を使用してください。
	// ライフサイクル操作にはhost_tools（.sandbox/host-tools/）を使用してください。
	if isDockerCommand(baseCmd) {
		return false, fmt.Errorf("docker commands are not supported in exec_host_command: use MCP tools (get_logs, get_stats, etc.) for monitoring, or host_tools for lifecycle operations")
	}

	// Check deny list first (deny overrides whitelist)
	// 拒否リストを先にチェック（拒否はホワイトリストを上書き）
	if p.matchesDenyList(baseCmd, args) {
		return false, fmt.Errorf("command denied: %s", command)
	}

	// Check whitelist
	// ホワイトリストをチェック
	if !p.matchesWhitelist(baseCmd, args) {
		// Check if available in dangerously mode
		// 危険モードで利用可能か確認
		if p.config.Dangerously.Enabled && p.matchesDangerousList(baseCmd, args) {
			return false, fmt.Errorf("command not whitelisted: %s (hint: this command is available with dangerously=true)", command)
		}
		return false, fmt.Errorf("command not whitelisted: %s", command)
	}

	return true, nil
}

// CanExecHostCommandDangerously checks if a command is allowed in dangerous mode.
// CanExecHostCommandDangerouslyは危険モードでコマンドが許可されているかチェックします。
func (p *HostCommandPolicy) CanExecHostCommandDangerously(command string) (bool, error) {
	if !p.config.Enabled {
		return false, fmt.Errorf("host commands are disabled")
	}

	if !p.config.Dangerously.Enabled {
		return false, fmt.Errorf("dangerous mode is not enabled for host commands")
	}

	// Check for pipes and redirects
	// パイプとリダイレクトのチェック
	if containsShellMetaChars(command) {
		return false, fmt.Errorf("shell meta-characters are not allowed: %s", command)
	}

	// Parse the command
	// コマンドを解析
	baseCmd, args, err := splitCommand(command)
	if err != nil {
		return false, fmt.Errorf("failed to parse command: %w", err)
	}

	// Reject docker/docker-compose commands (same as normal mode).
	// docker/docker-composeコマンドを拒否します（通常モードと同様）。
	if isDockerCommand(baseCmd) {
		return false, fmt.Errorf("docker commands are not supported in exec_host_command: use MCP tools (get_logs, get_stats, etc.) for monitoring, or host_tools for lifecycle operations")
	}

	// Check deny list first
	// 拒否リストを先にチェック
	if p.matchesDenyList(baseCmd, args) {
		return false, fmt.Errorf("command denied: %s", command)
	}

	// Check path traversal
	// パストラバーサルをチェック
	if containsPathTraversal(args) {
		return false, fmt.Errorf("path traversal detected: %s", command)
	}

	// First check normal whitelist (no need for dangerously flag if already whitelisted)
	// 通常ホワイトリストを先にチェック（既にホワイトリストにあればdangerouslyフラグ不要）
	if p.matchesWhitelist(baseCmd, args) {
		return true, nil
	}

	// Check dangerous commands list
	// 危険コマンドリストをチェック
	if !p.matchesDangerousList(baseCmd, args) {
		return false, fmt.Errorf("command not allowed in dangerous mode: %s", command)
	}

	return true, nil
}

// GetAllowedHostCommands returns the whitelist of allowed host commands.
// GetAllowedHostCommandsは許可されたホストコマンドのホワイトリストを返します。
func (p *HostCommandPolicy) GetAllowedHostCommands() map[string][]string {
	return p.config.Whitelist
}

// matchesWhitelist checks if a command matches any pattern in the whitelist.
// matchesWhitelistはコマンドがホワイトリスト内のパターンにマッチするかチェックします。
func (p *HostCommandPolicy) matchesWhitelist(baseCmd, args string) bool {
	patterns, ok := p.config.Whitelist[baseCmd]
	if !ok {
		return false
	}
	return matchesPatterns(args, patterns)
}

// matchesDenyList checks if a command matches any pattern in the deny list.
// matchesDenyListはコマンドが拒否リスト内のパターンにマッチするかチェックします。
func (p *HostCommandPolicy) matchesDenyList(baseCmd, args string) bool {
	patterns, ok := p.config.Deny[baseCmd]
	if !ok {
		return false
	}
	return matchesPatterns(args, patterns)
}

// matchesDangerousList checks if a command matches the dangerous commands list.
// matchesDangerousListはコマンドが危険コマンドリストにマッチするかチェックします。
func (p *HostCommandPolicy) matchesDangerousList(baseCmd, args string) bool {
	subcommands, ok := p.config.Dangerously.Commands[baseCmd]
	if !ok {
		return false
	}

	// For dangerous mode, check if the first argument matches any allowed subcommand
	// 危険モードでは、最初の引数が許可されたサブコマンドにマッチするかチェック
	firstArg := strings.Fields(args)
	if len(firstArg) == 0 {
		return false
	}

	for _, sub := range subcommands {
		if firstArg[0] == sub {
			return true
		}
	}
	return false
}

// matchesPatterns checks if an args string matches any pattern.
// Uses prefix matching with * wildcard (same as Policy.matchesWhitelist).
//
// matchesPatternsは引数文字列がパターンにマッチするかチェックします。
// *ワイルドカードによるプレフィックスマッチングを使用します（Policy.matchesWhitelistと同じ）。
func matchesPatterns(args string, patterns []string) bool {
	args = strings.TrimSpace(args)

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)

		// Exact match
		if args == pattern {
			return true
		}

		// Wildcard matching
		if strings.Contains(pattern, "*") {
			prefix := strings.Split(pattern, "*")[0]
			if strings.HasPrefix(args, prefix) {
				return true
			}
		}
	}
	return false
}

// splitCommand splits a command string into base command and arguments.
// splitCommandはコマンド文字列をベースコマンドと引数に分割します。
func splitCommand(command string) (string, string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", "", fmt.Errorf("empty command")
	}

	parts := strings.SplitN(command, " ", 2)
	baseCmd := parts[0]
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	return baseCmd, args, nil
}

// containsShellMetaChars checks if the command contains shell meta-characters
// that could enable injection: pipes, redirects, command chaining,
// command substitution ($(), backticks), and newlines.
//
// containsShellMetaCharsはインジェクションを可能にするシェルメタ文字が
// コマンドに含まれているかチェックします：パイプ、リダイレクト、
// コマンドチェーン、コマンド置換（$()、バッククォート）、改行。
func containsShellMetaChars(command string) bool {
	if strings.Contains(command, "$(") {
		return true
	}
	for _, ch := range command {
		switch ch {
		case '|', '>', '<', ';', '&', '`', '\n':
			return true
		}
	}
	return false
}

// containsPathTraversal checks if any argument contains path traversal.
// containsPathTraversalは引数にパストラバーサルが含まれているかチェックします。
func containsPathTraversal(args string) bool {
	return strings.Contains(args, "..")
}

// isDockerCommand checks if the base command is docker or docker-compose.
// isDockerCommandはベースコマンドがdockerまたはdocker-composeかチェックします。
func isDockerCommand(baseCmd string) bool {
	return baseCmd == "docker" || baseCmd == "docker-compose"
}
