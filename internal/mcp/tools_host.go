// Package mcp provides host tool and host command MCP handlers.
// These handlers enable AI assistants to discover and execute tools on the host OS,
// and to run whitelisted CLI commands on the host.
//
// mcpパッケージはホストツールおよびホストコマンドのMCPハンドラーを提供します。
// これらのハンドラーにより、AIアシスタントがホストOS上のツールを検出・実行し、
// ホワイトリストに登録されたCLIコマンドをホスト上で実行できるようになります。
package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YujiSuzuki/hostmcp/internal/hosttools"
)

// GetHostTools returns the MCP tool definitions for host tool operations.
// These are appended to the main tool list when host tools are enabled.
//
// GetHostToolsはホストツール操作のMCPツール定義を返します。
// ホストツールが有効な場合、メインのツールリストに追加されます。
func GetHostTools() []Tool {
	return []Tool{
		{
			Name:        "list_host_tools",
			Description: "List available tools in .sandbox/host-tools/ with descriptions and execution environment info",
			InputSchema: ToolInputSchema{
				Type:       "object",
				Properties: map[string]ToolProperty{},
			},
		},
		{
			Name:        "get_host_tool_info",
			Description: "Get detailed information about a specific host tool including usage, options, and execution environment",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"name": {
						Type:        "string",
						Description: "Tool filename (e.g. my-tool.sh)",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "run_host_tool",
			Description: "Execute a host tool. Host tools are scripts/programs in configured directories on the host OS.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"name": {
						Type:        "string",
						Description: "Tool filename (e.g. my-tool.sh)",
					},
					"args": {
						Type:        "array",
						Description: "Arguments to pass to the tool",
						Items:       &ToolPropertyItems{Type: "string"},
					},
					"client_timeout_seconds": {
						Type:        "number",
						Description: "Seconds you (the calling MCP client) will wait for this call's response — e.g. your MCP_TOOL_TIMEOUT (ms, divide by 1000) or hostmcp client --timeout value. Required only when this tool's own execution timeout exceeds the server's global default (host_access.host_tools.timeout); omit it for ordinary, fast tools. When required and too low, the server refuses to run the tool rather than executing it on the host only for you to give up waiting before the result arrives.",
					},
				},
				Required: []string{"name"},
			},
		},
	}
}

// toolListHostTools implements the list_host_tools MCP tool.
// toolListHostToolsはlist_host_tools MCPツールを実装します。
func (s *Server) toolListHostTools(ctx context.Context, args map[string]any) (any, error) {
	if s.hostToolsManager == nil {
		return nil, fmt.Errorf("host tools are not configured")
	}

	slog.Debug("Listing host tools")
	tools, err := s.hostToolsManager.ListTools()
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"tools": tools,
		"count": len(tools),
	}
	return jsonTextResponse(result)
}

// toolGetHostToolInfo implements the get_host_tool_info MCP tool.
// toolGetHostToolInfoはget_host_tool_info MCPツールを実装します。
func (s *Server) toolGetHostToolInfo(ctx context.Context, args map[string]any) (any, error) {
	if s.hostToolsManager == nil {
		return nil, fmt.Errorf("host tools are not configured")
	}

	name, ok := args["name"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid name parameter")
	}

	slog.Debug("Getting host tool info", "name", name)
	info, err := s.hostToolsManager.GetToolInfo(name)
	if err != nil {
		return nil, err
	}

	return jsonTextResponse(info)
}

// toolRunHostTool implements the run_host_tool MCP tool.
// toolRunHostToolはrun_host_tool MCPツールを実装します。
func (s *Server) toolRunHostTool(ctx context.Context, args map[string]any) (any, error) {
	if s.hostToolsManager == nil {
		return nil, fmt.Errorf("host tools are not configured")
	}

	name, ok := args["name"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid name parameter")
	}

	if err := s.checkClientTimeout(name, args); err != nil {
		return nil, err
	}

	// Extract optional args parameter
	// オプションのargsパラメータを抽出
	var toolArgs []string
	if argsRaw, ok := args["args"].([]any); ok {
		for _, a := range argsRaw {
			if s, ok := a.(string); ok {
				toolArgs = append(toolArgs, s)
			}
		}
	}

	slog.Info("Running host tool", "name", name, "args", toolArgs)
	result, err := s.hostToolsManager.RunTool(name, toolArgs)
	if err != nil {
		if strings.Contains(err.Error(), "execution timed out") {
			return nil, fmt.Errorf("%w\n\n%s", err, s.timeoutHint(name))
		}
		return nil, err
	}

	// Apply output masking and host path masking
	// 出力マスキングとホストパスマスキングを適用
	output := result.String()
	output = s.docker.GetPolicy().MaskExec(output)
	output = s.docker.GetPolicy().MaskHostPaths(output)

	// Check if output exceeds the large output threshold
	// 出力が大きな出力閾値を超えるかチェック
	cfg := s.hostToolsManager.Config()
	if cfg != nil && cfg.MaxOutputBytes > 0 && int64(len(output)) > cfg.MaxOutputBytes {
		content, saveErr := s.saveLargeToolOutput(name, output, result.ExitCode, cfg.LargeOutputDir)
		if saveErr != nil {
			slog.Warn("Failed to save large tool output to file, returning full output",
				"name", name, "error", saveErr)
			// Fall through to normal response with full (untruncated) output
			// ファイル保存失敗時はそのまま返す（切り詰めなし）
		} else {
			return textResponse(content), nil
		}
	}

	content := fmt.Sprintf("Tool: %s\nExit Code: %d\n\nOutput:\n%s", name, result.ExitCode, output)
	return textResponse(content), nil
}

// checkClientTimeout refuses to run a host tool whose own execution timeout
// exceeds the server's global default (host_access.host_tools.timeout)
// unless the caller has declared a client_timeout_seconds at least that
// large. Ordinary tools whose effective timeout stays within the global
// default are unaffected and need not declare it at all.
//
// Without this check, a client whose own wait budget (e.g. MCP_TOOL_TIMEOUT)
// is shorter than the tool's actual timeout would give up before the result
// arrives — the host-side execution still runs to completion, but the
// result is never delivered, wasting the run for nothing. This check
// catches that mismatch before the tool ever starts.
//
// A missing tool is not an error here: it's left for RunTool to report the
// same "tool not found" error it always has.
//
// checkClientTimeoutは、実効タイムアウトがサーバーのグローバル既定値
// （host_access.host_tools.timeout）を超えるホストツールについて、
// 呼び出し元がその値以上のclient_timeout_secondsを宣言していない限り
// 実行を拒否します。実効タイムアウトがグローバル既定値以内に収まる
// 通常のツールは影響を受けず、この宣言は一切不要です。
//
// このチェックが無いと、自身の待ち時間予算（例: MCP_TOOL_TIMEOUT）が
// ツールの実際のタイムアウトより短いクライアントは、結果が届く前に
// 諦めてしまいます——ホスト側の実行は最後まで完走しますが、結果が
// 届くことはなく、その実行が無駄になります。このチェックはツールが
// 実行される前にそのミスマッチを検知します。
//
// ツールが存在しない場合はここではエラーにせず、従来通りRunToolが
// 同じ「tool not found」エラーを返すようにします。
func (s *Server) checkClientTimeout(name string, args map[string]any) error {
	effective, err := s.hostToolsManager.EffectiveTimeout(name)
	if err != nil {
		return nil
	}

	cfg := s.hostToolsManager.Config()
	globalDefault := time.Duration(cfg.Timeout) * time.Second
	if effective <= globalDefault {
		return nil
	}

	effSec := int(effective.Seconds())
	globalSec := int(globalDefault.Seconds())

	// Both messages below spell out that client_timeout_seconds is a
	// self-reported value only — declaring it does not itself make the
	// caller's own connection wait any longer. A caller that just adds the
	// parameter without actually being able to wait that long (e.g. still
	// calling run_host_tool directly instead of through `hostmcp client
	// --timeout` in the background) will pass this check and then still lose
	// the result to its own real timeout, wasting the run anyway.
	//
	// 以下の両メッセージは、client_timeout_secondsが自己申告の値に過ぎず、
	// これを宣言すること自体は呼び出し元自身の接続の待ち時間を延ばさない、
	// という点を明記しています。実際にそこまで待てる手段（バックグラウンドで
	// の`hostmcp client --timeout`経由など）に切り替えずに、ただこの
	// パラメータを足しただけの呼び出し元は、このチェックは通過しても、
	// 結局自分自身の本当のタイムアウトで結果を失い、実行を無駄にします。
	raw, present := args["client_timeout_seconds"]
	clientTimeout, validNumber := raw.(float64)
	if !present || !validNumber || clientTimeout <= 0 {
		return fmt.Errorf(
			"this tool's execution timeout (%ds) exceeds the server default (%ds); pass client_timeout_seconds (>= %ds) so the server can confirm your MCP client will wait long enough — otherwise the tool would run on the host only for your client to give up before the result arrives. Note: client_timeout_seconds is a self-reported value only, it does not itself make your client wait longer — if calling run_host_tool directly won't actually wait %d seconds, call it via `hostmcp client --timeout %d` instead (e.g. in a backgrounded shell call) and only pass client_timeout_seconds=%d once you are actually calling it that way",
			effSec, globalSec, effSec, effSec, effSec, effSec)
	}

	if int(clientTimeout) < effSec {
		return fmt.Errorf(
			"declared client_timeout_seconds (%ds) is shorter than this tool's actual execution timeout (%ds); refusing to run it on the host to avoid wasting the run. Note: client_timeout_seconds is a self-reported value only, raising it here will not itself make your client wait longer — if calling run_host_tool directly won't actually wait %d seconds, call it via `hostmcp client --timeout %d` instead (e.g. in a backgrounded shell call) and only pass client_timeout_seconds=%d once you are actually calling it that way",
			int(clientTimeout), effSec, effSec, effSec, effSec)
	}

	return nil
}

// timeoutHint builds the guidance text appended to a timeout error, tailored
// to why the timeout happened: no per-tool declaration, a declaration that
// was honored as-is, or a declaration that was clamped by max_tool_timeout.
// The three cases need different advice — re-declaring a larger value only
// helps in the first two; in the clamped case it does nothing until an
// administrator raises max_tool_timeout in hostmcp.yaml.
//
// timeoutHintは、タイムアウトが発生した理由（ツール別宣言が無い・宣言どおり
// 使われた・max_tool_timeoutでクランプされた）に応じて、タイムアウトエラーに
// 添えるガイダンス文言を組み立てます。この3パターンでは有効な対処法が異なり、
// より大きい値を再宣言することは最初の2パターンでしか効果がありません
// — クランプされたケースでは、管理者がhostmcp.yamlのmax_tool_timeoutを
// 引き上げるまで無効です。
func (s *Server) timeoutHint(name string) string {
	cfg := s.hostToolsManager.Config()
	timeoutSec := 60
	maxToolTimeoutSec := 0
	if cfg != nil {
		timeoutSec = cfg.Timeout
		maxToolTimeoutSec = cfg.MaxToolTimeout
	}

	info, infoErr := s.hostToolsManager.GetToolInfo(name)
	declared := infoErr == nil && info.Timeout > 0

	base := fmt.Sprintf("To increase the timeout, update host_access.host_tools.timeout in hostmcp.yaml (current: %ds)", timeoutSec)

	switch {
	case declared && maxToolTimeoutSec > 0 && info.Timeout > maxToolTimeoutSec:
		return fmt.Sprintf(
			"%s\n\nThis tool declares \"@timeout: %d\" in its header, but that is capped by the max_tool_timeout ceiling (%ds) — the actual run was cut off at %ds, not %ds. Increasing the tool's own @timeout value will not help; an administrator must raise host_access.host_tools.max_tool_timeout in hostmcp.yaml.",
			base, info.Timeout, maxToolTimeoutSec, maxToolTimeoutSec, info.Timeout)
	case declared:
		ceiling := "no ceiling configured"
		if maxToolTimeoutSec > 0 {
			ceiling = fmt.Sprintf("max_tool_timeout: %ds", maxToolTimeoutSec)
		}
		return fmt.Sprintf(
			"%s\n\nThis tool already declares \"@timeout: %d\" in its header. Increase that value and get it re-approved with `hostmcp tools sync` (current ceiling, %s).",
			base, info.Timeout, ceiling)
	default:
		return fmt.Sprintf(
			"%s\n\nAlternatively, to extend the timeout for just this tool, add \"# @timeout: <seconds>\" to its header comment and get it approved with `hostmcp tools sync`.",
			base)
	}
}

// saveLargeToolOutput saves large tool output to a file and returns a summary message.
// The file is saved to <workspaceRoot>/<largeOutputDir>/hostmcp-<toolname>-last.log.
//
// saveLargeToolOutputは大きなツール出力をファイルに保存し、サマリーメッセージを返します。
// ファイルは <workspaceRoot>/<largeOutputDir>/hostmcp-<toolname>-last.log に保存されます。
func (s *Server) saveLargeToolOutput(name, output string, exitCode int, largeOutputDir string) (string, error) {
	// Derive a safe filename from the tool name (strip extension)
	// ツール名からファイル名を生成（拡張子を除去）
	base := name
	if idx := strings.LastIndex(base, "."); idx >= 0 {
		base = base[:idx]
	}
	filename := fmt.Sprintf("hostmcp-%s-last.log", base)

	// Build the output directory path
	// 出力ディレクトリパスを構築
	dir := largeOutputDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(s.workspaceRoot, dir)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create large output directory %s: %w", dir, err)
	}

	logPath := filepath.Join(dir, filename)
	if err := os.WriteFile(logPath, []byte(output), 0644); err != nil {
		return "", fmt.Errorf("failed to write large output to %s: %w", logPath, err)
	}

	slog.Info("Large tool output saved to file", "name", name, "path", logPath, "bytes", len(output))

	// Build a preview: head 20 lines + tail 20 lines
	// プレビューを構築: 先頭20行 + 末尾20行
	lines := strings.Split(output, "\n")
	const previewLines = 20
	var preview strings.Builder
	if len(lines) <= previewLines*2 {
		preview.WriteString(output)
	} else {
		for _, l := range lines[:previewLines] {
			preview.WriteString(l)
			preview.WriteByte('\n')
		}
		fmt.Fprintf(&preview, "\n... (%d lines omitted) ...\n\n", len(lines)-previewLines*2)
		for _, l := range lines[len(lines)-previewLines:] {
			preview.WriteString(l)
			preview.WriteByte('\n')
		}
	}

	// Mask host paths in the log path shown to AI
	// AIに表示するログパスのホストパスをマスク
	maskedPath := s.docker.GetPolicy().MaskHostPaths(logPath)

	content := fmt.Sprintf(
		"Tool: %s\nExit Code: %d\n\n"+
			"⚠️  Output was large (%d bytes) and has been saved to a file.\n"+
			"File: %s\n"+
			"Use the Read or Grep tool to inspect the full output.\n\n"+
			"--- Preview (first/last %d lines) ---\n%s",
		name, exitCode, len(output), maskedPath, previewLines, preview.String())

	return content, nil
}

// GetHostCommandTools returns the MCP tool definitions for host command operations.
// These are appended to the main tool list when host commands are enabled.
//
// GetHostCommandToolsはホストコマンド操作のMCPツール定義を返します。
// ホストコマンドが有効な場合、メインのツールリストに追加されます。
func GetHostCommandTools() []Tool {
	return []Tool{
		{
			Name:        "exec_host_command",
			Description: "Execute a whitelisted CLI command on the host OS. Commands must be configured in the host_commands whitelist. Use dangerously=true for commands in the dangerously list.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"command": {
						Type:        "string",
						Description: "Command to execute (must be whitelisted in config)",
					},
					"dangerously": {
						Type:        "boolean",
						Description: "Enable dangerous mode to execute commands from the dangerously list",
					},
				},
				Required: []string{"command"},
			},
		},
	}
}

// toolExecHostCommand implements the exec_host_command MCP tool.
// toolExecHostCommandはexec_host_command MCPツールを実装します。
func (s *Server) toolExecHostCommand(ctx context.Context, args map[string]any) (any, error) {
	if s.hostCommandPolicy == nil {
		return nil, fmt.Errorf("host commands are not configured")
	}

	command, ok := args["command"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid command parameter")
	}

	dangerously := false
	if d, ok := args["dangerously"].(bool); ok {
		dangerously = d
	}

	// Check security policy
	// セキュリティポリシーをチェック
	var allowed bool
	var err error
	if dangerously {
		slog.Warn("Executing host command (DANGEROUS MODE)", "command", command)
		allowed, err = s.hostCommandPolicy.CanExecHostCommandDangerously(command)
	} else {
		slog.Info("Executing host command", "command", command)
		allowed, err = s.hostCommandPolicy.CanExecHostCommand(command)
	}

	if err != nil {
		slog.Warn("Host command blocked", "command", command, "dangerously", dangerously, "error", err.Error())
		return nil, err
	}
	if !allowed {
		slog.Warn("Host command not allowed", "command", command, "dangerously", dangerously)
		return nil, fmt.Errorf("command not allowed: %s", command)
	}

	// Defensive check: workspaceRoot must be set (should be caught by config.Validate)
	// 防御的チェック: workspaceRootが設定されている必要がある（config.Validateで検出されるはず）
	if s.workspaceRoot == "" {
		return nil, fmt.Errorf("workspace root is not configured")
	}

	// Execute the command
	// コマンドを実行
	result, err := hosttools.ExecHostCommand(command, s.workspaceRoot, s.hostCommandTimeout)
	if err != nil {
		return nil, err
	}

	// Apply output masking
	// 出力マスキングを適用
	output := result.String()
	output = s.docker.GetPolicy().MaskExec(output)
	output = s.docker.GetPolicy().MaskHostPaths(output)

	// Add warning for dangerous mode
	// 危険モードの場合は警告を追加
	prefix := ""
	if dangerously {
		prefix = "⚠️ [DANGEROUS MODE] "
	}

	content := fmt.Sprintf("%sCommand: %s\nExit Code: %d\n\nOutput:\n%s",
		prefix, command, result.ExitCode, output)
	return textResponse(content), nil
}
