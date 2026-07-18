// client_test.go contains unit tests for the client command and its subcommands.
// These tests verify that client commands are properly registered with correct configuration.
//
// client_test.goはclientコマンドとそのサブコマンドのユニットテストを含みます。
// これらのテストはクライアントコマンドが正しい設定で適切に登録されていることを確認します。
//
// IMPORTANT: Scope and Limitations of These Tests
// 重要: これらのテストのスコープと制限事項
//
// Tests like TestClientCommand, TestClientListCommand, etc. verify REGISTRATION only:
// TestClientCommand、TestClientListCommand等のテストは登録のみを検証します：
//
//   - ✅ Subcommand exists (サブコマンドが存在する)
//   - ✅ Flag configuration (フラグ設定)
//   - ❌ Actual HTTP/SSE communication (実際のHTTP/SSE通信)
//   - ❌ RunE execution logic (RunE実行ロジック)
//
// However, this file also contains meaningful tests:
// ただし、このファイルには意味のあるテストも含まれています：
//
//   - TestServerURLEnvVar: Tests environment variable priority logic
//     環境変数の優先順位ロジックをテスト
//   - TestServerURLConfigPortFallback: Tests the config-derived server.port
//     fallback and its precedence against --url/HOSTMCP_SERVER_URL
//     config由来のserver.portフォールバックと、--url/HOSTMCP_SERVER_URLに
//     対する優先順位をテスト
//   - TestServerURLConfigPortFallbackLogsSource: Tests that the resolved
//     config path is logged to stderr only when the config-derived port is used
//     config由来のポートが使われた場合のみ、解決したconfigパスがstderrに
//     出力されることをテスト
//   - TestResolveClientServerPort: Tests resolveClientServerPort's workspace
//     resolution order and lenient failure behavior
//     resolveClientServerPortのワークスペース解決順序と寛容な失敗時の
//     挙動をテスト
//   - TestIsJapaneseLocale: Tests LC_ALL/LANG precedence and the "ja_JP"
//     prefix check used for locale-aware flag help text
//     ロケール依存のフラグヘルプ文字列に使うLC_ALL/LANGの優先順位と
//     "ja_JP"接頭辞判定をテスト
//   - TestExtractJSONFromMarkdown: Tests JSON extraction from markdown
//     MarkdownからのJSON抽出をテスト
//
// The actual client communication is tested in internal/client/client_test.go
// which covers SSE connections, tool calls, and error handling.
// 実際のクライアント通信はinternal/client/client_test.goでテストされ、
// SSE接続、ツール呼び出し、エラーハンドリングがカバーされています。
package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestClientCommand verifies that the client command is properly configured.
// It checks the command name and the default URL flag value.
//
// TestClientCommandはclientコマンドが適切に設定されていることを確認します。
// コマンド名とデフォルトのURLフラグ値を確認します。
func TestClientCommand(t *testing.T) {
	// Test that client command is registered.
	// clientコマンドが登録されていることをテストします。
	if clientCmd == nil {
		t.Fatal("clientCmd is nil")
	}

	// Verify the command usage string.
	// コマンドの使用方法文字列を確認します。
	if clientCmd.Use != "client" {
		t.Errorf("Expected Use to be 'client', got %s", clientCmd.Use)
	}

	// Test default URL flag has a value.
	// The actual default is "http://host.docker.internal:18080".
	//
	// デフォルトのURLフラグが値を持っていることをテストします。
	// 実際のデフォルトは"http://host.docker.internal:18080"です。
	if serverURL == "" {
		t.Error("serverURL should have a default value")
	}
}

// TestClientURLFlag verifies that the --url flag is properly configured on client command.
// This is a persistent flag that applies to all subcommands.
//
// TestClientURLFlagはclientコマンドの--urlフラグが適切に設定されていることを確認します。
// これはすべてのサブコマンドに適用される永続フラグです。
func TestClientURLFlag(t *testing.T) {
	// Check that url flag exists as persistent flag.
	// urlフラグが永続フラグとして存在することを確認します。
	flag := clientCmd.PersistentFlags().Lookup("url")
	if flag == nil {
		t.Fatal("url flag not found on clientCmd")
	}

	// Verify the default value is the expected HostMCP server URL.
	// デフォルト値が期待されるHostMCPサーバーURLであることを確認します。
	expectedDefault := "http://host.docker.internal:18080"
	if flag.DefValue != expectedDefault {
		t.Errorf("Expected url flag default to be '%s', got %s", expectedDefault, flag.DefValue)
	}

	// Verify the flag type is string.
	// フラグの型がstringであることを確認します。
	if flag.Value.Type() != "string" {
		t.Errorf("Expected url flag type to be 'string', got %s", flag.Value.Type())
	}
}

// Verifies LC_ALL/LANG precedence and the "ja_JP" prefix check used to pick
// between English and Japanese flag help text in init().
//
// init()内のフラグヘルプ文字列を英語/日本語で切り替えるために使う
// LC_ALL/LANGの優先順位と"ja_JP"接頭辞判定を確認します。
func TestIsJapaneseLocale(t *testing.T) {
	tests := []struct {
		name     string
		lcAll    string
		lang     string
		expected bool
	}{
		{"LC_ALL ja_JP takes precedence over non-Japanese LANG", "ja_JP.UTF-8", "en_US.UTF-8", true},
		{"LANG ja_JP used when LC_ALL is unset", "", "ja_JP.UTF-8", true},
		{"neither is Japanese", "en_US.UTF-8", "en_US.UTF-8", false},
		{"both unset", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLCAll := os.Getenv("LC_ALL")
			originalLang := os.Getenv("LANG")
			defer func() {
				os.Setenv("LC_ALL", originalLCAll)
				os.Setenv("LANG", originalLang)
			}()

			os.Setenv("LC_ALL", tt.lcAll)
			os.Setenv("LANG", tt.lang)

			if got := isJapaneseLocale(); got != tt.expected {
				t.Errorf("isJapaneseLocale() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestClientSubcommands verifies that all expected subcommands are registered.
// This ensures the client command group is complete.
//
// TestClientSubcommandsはすべての期待されるサブコマンドが登録されていることを確認します。
// これによりclientコマンドグループが完全であることを確認します。
func TestClientSubcommands(t *testing.T) {
	// Define the list of expected subcommands.
	// 期待されるサブコマンドのリストを定義します。
	expectedSubcommands := []string{"list", "logs", "exec", "stats", "inspect", "restart", "stop", "start", "host-tools", "host-exec"}

	// Get all registered subcommands under client.
	// client配下のすべての登録されたサブコマンドを取得します。
	commands := clientCmd.Commands()

	// Build a map of command names for lookup.
	// 検索用のコマンド名のマップを構築します。
	commandNames := make(map[string]bool)
	for _, cmd := range commands {
		// Extract first word from Use (e.g., "logs CONTAINER" -> "logs").
		// Useから最初の単語を抽出します（例："logs CONTAINER" -> "logs"）。
		use := cmd.Use
		if len(use) > 0 {
			for i, ch := range use {
				if ch == ' ' {
					use = use[:i]
					break
				}
			}
		}
		commandNames[use] = true
	}

	// Verify each expected subcommand is registered.
	// 期待される各サブコマンドが登録されていることを確認します。
	for _, expected := range expectedSubcommands {
		if !commandNames[expected] {
			t.Errorf("Missing expected subcommand: %s", expected)
		}
	}
}

// Verifies that the client list subcommand is properly configured.
//
// client listサブコマンドが適切に設定されていることを確認します。
func TestClientListCommand(t *testing.T) {
	// Test that client list command is registered.
	// client listコマンドが登録されていることをテストします。
	if clientListCmd == nil {
		t.Fatal("clientListCmd is nil")
	}

	// Verify the command usage string.
	// コマンドの使用方法文字列を確認します。
	if clientListCmd.Use != "list" {
		t.Errorf("Expected Use to be 'list', got %s", clientListCmd.Use)
	}
}

// Verifies that the client logs subcommand is properly configured.
//
// client logsサブコマンドが適切に設定されていることを確認します。
func TestClientLogsCommand(t *testing.T) {
	// Test that client logs command is registered.
	// client logsコマンドが登録されていることをテストします。
	if clientLogsCmd == nil {
		t.Fatal("clientLogsCmd is nil")
	}

	// Verify the command usage string includes CONTAINER argument.
	// コマンドの使用方法文字列がCONTAINER引数を含むことを確認します。
	if clientLogsCmd.Use != "logs CONTAINER" {
		t.Errorf("Expected Use to be 'logs CONTAINER', got %s", clientLogsCmd.Use)
	}
}

// Verifies that the --tail flag is properly configured on client logs command.
//
// client logsコマンドの--tailフラグが適切に設定されていることを確認します。
func TestClientLogsTailFlag(t *testing.T) {
	// Check that tail flag exists.
	// tailフラグが存在することを確認します。
	flag := clientLogsCmd.Flags().Lookup("tail")
	if flag == nil {
		t.Fatal("tail flag not found on clientLogsCmd")
	}

	// Verify the default value is 100.
	// デフォルト値が100であることを確認します。
	if flag.DefValue != "100" {
		t.Errorf("Expected tail flag default to be '100', got %s", flag.DefValue)
	}

	// Verify the flag type is int.
	// フラグの型がintであることを確認します。
	if flag.Value.Type() != "int" {
		t.Errorf("Expected tail flag type to be 'int', got %s", flag.Value.Type())
	}
}

// Verifies that the --since flag is properly configured on client logs command.
//
// client logsコマンドの--sinceフラグが適切に設定されていることを確認します。
func TestClientLogsSinceFlag(t *testing.T) {
	// Check that since flag exists.
	// sinceフラグが存在することを確認します。
	flag := clientLogsCmd.Flags().Lookup("since")
	if flag == nil {
		t.Fatal("since flag not found on clientLogsCmd")
	}

	// Verify the default value is empty string.
	// デフォルト値が空文字列であることを確認します。
	if flag.DefValue != "" {
		t.Errorf("Expected since flag default to be empty, got %s", flag.DefValue)
	}

	// Verify the flag type is string.
	// フラグの型がstringであることを確認します。
	if flag.Value.Type() != "string" {
		t.Errorf("Expected since flag type to be 'string', got %s", flag.Value.Type())
	}
}

// Verifies that the --follow flag is properly configured on client logs command.
//
// client logsコマンドの--followフラグが適切に設定されていることを確認します。
func TestClientLogsFollowFlag(t *testing.T) {
	// Check that follow flag exists.
	// followフラグが存在することを確認します。
	flag := clientLogsCmd.Flags().Lookup("follow")
	if flag == nil {
		t.Fatal("follow flag not found on clientLogsCmd")
	}

	// Verify the default value is false.
	// デフォルト値がfalseであることを確認します。
	if flag.DefValue != "false" {
		t.Errorf("Expected follow flag default to be 'false', got %s", flag.DefValue)
	}

	// Verify the flag type is bool.
	// フラグの型がboolであることを確認します。
	if flag.Value.Type() != "bool" {
		t.Errorf("Expected follow flag type to be 'bool', got %s", flag.Value.Type())
	}
}

// Verifies that the client exec subcommand is properly configured.
//
// client execサブコマンドが適切に設定されていることを確認します。
func TestClientExecCommand(t *testing.T) {
	// Test that client exec command is registered.
	// client execコマンドが登録されていることをテストします。
	if clientExecCmd == nil {
		t.Fatal("clientExecCmd is nil")
	}

	// Verify the command usage string includes CONTAINER and COMMAND arguments.
	// コマンドの使用方法文字列がCONTAINERとCOMMAND引数を含むことを確認します。
	if clientExecCmd.Use != "exec CONTAINER COMMAND" {
		t.Errorf("Expected Use to be 'exec CONTAINER COMMAND', got %s", clientExecCmd.Use)
	}
}

// Verifies that the client stats subcommand is properly configured.
//
// client statsサブコマンドが適切に設定されていることを確認します。
func TestClientStatsCommand(t *testing.T) {
	// Test that client stats command is registered.
	// client statsコマンドが登録されていることをテストします。
	if clientStatsCmd == nil {
		t.Fatal("clientStatsCmd is nil")
	}

	// Verify the command usage string includes CONTAINER argument.
	// コマンドの使用方法文字列がCONTAINER引数を含むことを確認します。
	if clientStatsCmd.Use != "stats CONTAINER" {
		t.Errorf("Expected Use to be 'stats CONTAINER', got %s", clientStatsCmd.Use)
	}
}

// Verifies that the client inspect subcommand is properly configured.
//
// client inspectサブコマンドが適切に設定されていることを確認します。
func TestClientInspectCommand(t *testing.T) {
	// Test that client inspect command is registered.
	// client inspectコマンドが登録されていることをテストします。
	if clientInspectCmd == nil {
		t.Fatal("clientInspectCmd is nil")
	}

	// Verify the command usage string includes CONTAINER argument.
	// コマンドの使用方法文字列がCONTAINER引数を含むことを確認します。
	if clientInspectCmd.Use != "inspect CONTAINER" {
		t.Errorf("Expected Use to be 'inspect CONTAINER', got %s", clientInspectCmd.Use)
	}
}

// TestClientInspectJSONFlag verifies that the --json flag is properly configured on client inspect command.
// Default is false (summary mode), --json outputs full JSON.
//
// TestClientInspectJSONFlagはclient inspectコマンドの--jsonフラグが適切に設定されていることを確認します。
// デフォルトはfalse（サマリーモード）、--jsonはフルJSONを出力します。
func TestClientInspectJSONFlag(t *testing.T) {
	// Check that json flag exists.
	// jsonフラグが存在することを確認します。
	flag := clientInspectCmd.Flags().Lookup("json")
	if flag == nil {
		t.Fatal("json flag not found on clientInspectCmd")
	}

	// Verify the default value is false (summary mode).
	// デフォルト値がfalse（サマリーモード）であることを確認します。
	if flag.DefValue != "false" {
		t.Errorf("Expected json flag default to be 'false', got %s", flag.DefValue)
	}

	// Verify the flag type is bool.
	// フラグの型がboolであることを確認します。
	if flag.Value.Type() != "bool" {
		t.Errorf("Expected json flag type to be 'bool', got %s", flag.Value.Type())
	}
}

// TestClientSuffixEnvVar verifies that HOSTMCP_CLIENT_SUFFIX environment variable is used
// as a fallback when --client-suffix flag is not explicitly set.
//
// HOSTMCP_CLIENT_SUFFIX環境変数が、--client-suffixフラグが明示的に設定されていない場合に
// フォールバックとして使用されることを確認します。
func TestClientSuffixEnvVar(t *testing.T) {
	tests := []struct {
		name           string
		envValue       string // HOSTMCP_CLIENT_SUFFIX env var value
		flagArgs       []string
		expectedSuffix string
	}{
		{
			name:           "env var used when flag not set",
			envValue:       "from-env",
			flagArgs:       nil,
			expectedSuffix: "from-env",
		},
		{
			name:           "flag takes precedence over env var",
			envValue:       "from-env",
			flagArgs:       []string{"--client-suffix", "from-flag"},
			expectedSuffix: "from-flag",
		},
		{
			name:           "no suffix when neither set",
			envValue:       "",
			flagArgs:       nil,
			expectedSuffix: "",
		},
		{
			name:           "empty flag overrides env var",
			envValue:       "from-env",
			flagArgs:       []string{"--client-suffix", ""},
			expectedSuffix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore original state.
			// 元の状態を保存して復元します。
			originalSuffix := clientSuffix
			originalEnv := os.Getenv("HOSTMCP_CLIENT_SUFFIX")
			originalURL := serverURL
			originalCfgFile := cfgFile
			originalWorkspaceEnv := os.Getenv("WORKSPACE")
			defer func() {
				clientSuffix = originalSuffix
				os.Setenv("HOSTMCP_CLIENT_SUFFIX", originalEnv)
				serverURL = originalURL
				cfgFile = originalCfgFile
				os.Setenv("WORKSPACE", originalWorkspaceEnv)
			}()

			// Reset clientSuffix to default (empty).
			// clientSuffixをデフォルト（空）にリセットします。
			clientSuffix = ""

			// This test's fresh cmd below has no "url" flag registered, so
			// PersistentPreRunE's cmd.Flags().Changed("url") is always false
			// here, meaning the URL/config-port fallback branch still runs
			// even though this test isn't about URLs. Point $WORKSPACE at an
			// empty temp dir so it doesn't read the real
			// /workspace/.sandbox/config/hostmcp.yaml as a side effect.
			//
			// このテストの下記の新しいcmdには"url"フラグが登録されていないため、
			// PersistentPreRunEのcmd.Flags().Changed("url")は常にfalseになり、
			// このテストがURLに関するものでなくてもURL/config-portフォールバック
			// 分岐が実行されてしまいます。実際の
			// /workspace/.sandbox/config/hostmcp.yamlを副作用として読まないよう、
			// $WORKSPACEを空の一時ディレクトリに向けます。
			cfgFile = ""
			os.Setenv("WORKSPACE", t.TempDir())

			// Set environment variable.
			// 環境変数を設定します。
			if tt.envValue != "" {
				os.Setenv("HOSTMCP_CLIENT_SUFFIX", tt.envValue)
			} else {
				os.Unsetenv("HOSTMCP_CLIENT_SUFFIX")
			}

			// Create a fresh command to avoid flag state leaking between tests.
			// テスト間でフラグの状態が漏れないように新しいコマンドを作成します。
			cmd := &cobra.Command{Use: "test"}
			cmd.PersistentFlags().StringVarP(&clientSuffix, "client-suffix", "s", "", "")
			if tt.flagArgs != nil {
				cmd.SetArgs(tt.flagArgs)
				cmd.ParseFlags(tt.flagArgs)
			}

			// Execute PersistentPreRunE directly.
			// PersistentPreRunEを直接実行します。
			err := clientCmd.PersistentPreRunE(cmd, []string{})
			if err != nil {
				t.Fatalf("PersistentPreRunE returned error: %v", err)
			}

			if clientSuffix != tt.expectedSuffix {
				t.Errorf("clientSuffix = %q, want %q", clientSuffix, tt.expectedSuffix)
			}
		})
	}
}

// TestServerURLEnvVar verifies that HOSTMCP_SERVER_URL environment variable is used
// as a fallback when --url flag is not explicitly set.
//
// HOSTMCP_SERVER_URL環境変数が、--urlフラグが明示的に設定されていない場合に
// フォールバックとして使用されることを確認します。
func TestServerURLEnvVar(t *testing.T) {
	const defaultURL = "http://host.docker.internal:18080"

	tests := []struct {
		name        string
		envValue    string // HOSTMCP_SERVER_URL env var value
		flagArgs    []string
		expectedURL string
	}{
		{
			name:        "env var used when flag not set",
			envValue:    "http://localhost:9090",
			flagArgs:    nil,
			expectedURL: "http://localhost:9090",
		},
		{
			name:        "flag takes precedence over env var",
			envValue:    "http://localhost:9090",
			flagArgs:    []string{"--url", "http://custom:7070"},
			expectedURL: "http://custom:7070",
		},
		{
			name:        "default used when neither set",
			envValue:    "",
			flagArgs:    nil,
			expectedURL: defaultURL,
		},
		{
			name:        "explicit flag value overrides env var even if same as default",
			envValue:    "http://localhost:9090",
			flagArgs:    []string{"--url", defaultURL},
			expectedURL: defaultURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore original state.
			// 元の状態を保存して復元します。
			originalURL := serverURL
			originalEnv := os.Getenv("HOSTMCP_SERVER_URL")
			originalCfgFile := cfgFile
			originalWorkspaceEnv := os.Getenv("WORKSPACE")
			defer func() {
				serverURL = originalURL
				os.Setenv("HOSTMCP_SERVER_URL", originalEnv)
				cfgFile = originalCfgFile
				os.Setenv("WORKSPACE", originalWorkspaceEnv)
			}()

			// Reset serverURL to default.
			// serverURLをデフォルトにリセットします。
			serverURL = defaultURL

			// Point the config-port fallback ($WORKSPACE) at an empty temp
			// dir (no hostmcp.yaml inside), so this test is isolated from
			// the real /workspace/.sandbox/config/hostmcp.yaml — this repo's
			// own devcontainer actually has that file (server.port: 8180),
			// and without this isolation the "default used when neither set"
			// case would pick up the real port instead of the hardcoded
			// default, since resolveClientServerPort falls back to
			// "/workspace" when cfgFile is empty and $WORKSPACE is unset.
			//
			// config-portフォールバック（$WORKSPACE）の参照先を、hostmcp.yamlが
			// 存在しない空の一時ディレクトリに向けます。これにより、実際の
			// /workspace/.sandbox/config/hostmcp.yaml（このリポジトリの
			// devcontainer自体が持つserver.port: 8180の設定ファイル）から
			// このテストを隔離します。この隔離がないと、
			// resolveClientServerPortはcfgFileが空で$WORKSPACEも未設定の場合
			// "/workspace"にフォールバックするため、「neither set」ケースが
			// ハードコードされたデフォルトではなく実際のポートを拾ってしまいます。
			cfgFile = ""
			os.Setenv("WORKSPACE", t.TempDir())

			// Set environment variable.
			// 環境変数を設定します。
			if tt.envValue != "" {
				os.Setenv("HOSTMCP_SERVER_URL", tt.envValue)
			} else {
				os.Unsetenv("HOSTMCP_SERVER_URL")
			}

			// Create a fresh command to avoid flag state leaking between tests.
			// テスト間でフラグの状態が漏れないように新しいコマンドを作成します。
			cmd := &cobra.Command{Use: "test"}
			cmd.PersistentFlags().StringVar(&serverURL, "url", defaultURL, "")
			if tt.flagArgs != nil {
				cmd.SetArgs(tt.flagArgs)
				cmd.ParseFlags(tt.flagArgs)
			}

			// Execute PersistentPreRunE directly.
			// PersistentPreRunEを直接実行します。
			err := clientCmd.PersistentPreRunE(cmd, []string{})
			if err != nil {
				t.Fatalf("PersistentPreRunE returned error: %v", err)
			}

			if serverURL != tt.expectedURL {
				t.Errorf("serverURL = %q, want %q", serverURL, tt.expectedURL)
			}
		})
	}
}

// Verifies that server.port from a resolved hostmcp.yaml is used as the URL
// fallback when neither --url nor HOSTMCP_SERVER_URL is set, and that it is
// correctly out-ranked by both.
//
// --urlもHOSTMCP_SERVER_URLも設定されていない場合にhostmcp.yamlから
// 解決したserver.portがURLフォールバックとして使われること、
// およびそれが両者より優先順位が低いことを確認します。
func TestServerURLConfigPortFallback(t *testing.T) {
	const defaultURL = "http://host.docker.internal:18080"

	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, ".sandbox", "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	cfgPath := filepath.Join(cfgDir, "hostmcp.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  port: 8180\n"), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	tests := []struct {
		name        string
		envValue    string
		flagArgs    []string
		expectedURL string
	}{
		{
			name:        "config port used when neither url flag nor env var set",
			envValue:    "",
			flagArgs:    nil,
			expectedURL: "http://host.docker.internal:8180",
		},
		{
			name:        "env var takes precedence over config port",
			envValue:    "http://localhost:9090",
			flagArgs:    nil,
			expectedURL: "http://localhost:9090",
		},
		{
			name:        "url flag takes precedence over config port",
			envValue:    "",
			flagArgs:    []string{"--url", "http://custom:7070"},
			expectedURL: "http://custom:7070",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalURL := serverURL
			originalEnv := os.Getenv("HOSTMCP_SERVER_URL")
			originalCfgFile := cfgFile
			originalWorkspaceEnv := os.Getenv("WORKSPACE")
			originalSuffix := clientSuffix
			originalTimeout := clientTimeout
			defer func() {
				serverURL = originalURL
				os.Setenv("HOSTMCP_SERVER_URL", originalEnv)
				cfgFile = originalCfgFile
				os.Setenv("WORKSPACE", originalWorkspaceEnv)
				clientSuffix = originalSuffix
				clientTimeout = originalTimeout
			}()

			serverURL = defaultURL
			cfgFile = ""
			os.Setenv("WORKSPACE", tmpDir)

			if tt.envValue != "" {
				os.Setenv("HOSTMCP_SERVER_URL", tt.envValue)
			} else {
				os.Unsetenv("HOSTMCP_SERVER_URL")
			}

			cmd := &cobra.Command{Use: "test"}
			cmd.PersistentFlags().StringVar(&serverURL, "url", defaultURL, "")
			if tt.flagArgs != nil {
				cmd.SetArgs(tt.flagArgs)
				cmd.ParseFlags(tt.flagArgs)
			}

			if err := clientCmd.PersistentPreRunE(cmd, []string{}); err != nil {
				t.Fatalf("PersistentPreRunE returned error: %v", err)
			}

			if serverURL != tt.expectedURL {
				t.Errorf("serverURL = %q, want %q", serverURL, tt.expectedURL)
			}
		})
	}
}

// Verifies that PersistentPreRunE prints the resolved hostmcp.yaml path to
// stderr whenever it uses the config-derived port, so an unexpected port
// picked up from a stray config file (e.g. at the fallback "/workspace"
// path) is traceable rather than silent. It must NOT print anything when
// --url or HOSTMCP_SERVER_URL already determined the URL, since
// resolveClientServerPort isn't even called in those cases.
//
// config由来のポートが使われる際にPersistentPreRunEが解決した
// hostmcp.yamlのパスをstderrに出力することを確認します。
// これにより、想定外の場所（フォールバック先の"/workspace"にたまたま存在した
// configなど）から拾ったポートも無警告にはなりません。--urlやHOSTMCP_SERVER_URL
// で既にURLが決まっている場合は、resolveClientServerPort自体が呼ばれないため
// 何も出力されないことも確認します。
func TestServerURLConfigPortFallbackLogsSource(t *testing.T) {
	const defaultURL = "http://host.docker.internal:18080"

	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, ".sandbox", "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	cfgPath := filepath.Join(cfgDir, "hostmcp.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  port: 8180\n"), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	tests := []struct {
		name       string
		envValue   string
		flagArgs   []string
		wantOutput bool
	}{
		{
			name:       "config port used: source path is logged",
			envValue:   "",
			flagArgs:   nil,
			wantOutput: true,
		},
		{
			name:       "env var wins: nothing logged",
			envValue:   "http://localhost:9090",
			flagArgs:   nil,
			wantOutput: false,
		},
		{
			name:       "url flag wins: nothing logged",
			envValue:   "",
			flagArgs:   []string{"--url", "http://custom:7070"},
			wantOutput: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalURL := serverURL
			originalEnv := os.Getenv("HOSTMCP_SERVER_URL")
			originalCfgFile := cfgFile
			originalWorkspaceEnv := os.Getenv("WORKSPACE")
			originalSuffix := clientSuffix
			originalTimeout := clientTimeout
			defer func() {
				serverURL = originalURL
				os.Setenv("HOSTMCP_SERVER_URL", originalEnv)
				cfgFile = originalCfgFile
				os.Setenv("WORKSPACE", originalWorkspaceEnv)
				clientSuffix = originalSuffix
				clientTimeout = originalTimeout
			}()

			serverURL = defaultURL
			cfgFile = ""
			os.Setenv("WORKSPACE", tmpDir)

			if tt.envValue != "" {
				os.Setenv("HOSTMCP_SERVER_URL", tt.envValue)
			} else {
				os.Unsetenv("HOSTMCP_SERVER_URL")
			}

			cmd := &cobra.Command{Use: "test"}
			cmd.PersistentFlags().StringVar(&serverURL, "url", defaultURL, "")
			if tt.flagArgs != nil {
				cmd.SetArgs(tt.flagArgs)
				cmd.ParseFlags(tt.flagArgs)
			}

			originalStderr := os.Stderr
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("failed to create pipe: %v", err)
			}
			os.Stderr = w

			preRunErr := clientCmd.PersistentPreRunE(cmd, []string{})

			w.Close()
			os.Stderr = originalStderr
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r); err != nil {
				t.Fatalf("failed to read captured stderr: %v", err)
			}
			captured := buf.String()

			if preRunErr != nil {
				t.Fatalf("PersistentPreRunE returned error: %v", preRunErr)
			}

			if tt.wantOutput {
				if !strings.Contains(captured, "8180") || !strings.Contains(captured, cfgPath) {
					t.Errorf("stderr = %q, want it to mention port 8180 and path %q", captured, cfgPath)
				}
			} else if captured != "" {
				t.Errorf("stderr = %q, want no output", captured)
			}
		})
	}
}

// Verifies resolveClientServerPort's workspace resolution order and its
// lenient (never-error) failure behavior.
//
// resolveClientServerPortのワークスペース解決順序と、寛容な
// （決してエラーを返さない）失敗時の挙動を確認します。
func TestResolveClientServerPort(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, ".sandbox", "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	cfgPath := filepath.Join(cfgDir, "hostmcp.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  port: 9199\n"), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	t.Run("explicit cfgFile is used directly", func(t *testing.T) {
		port, path, ok := resolveClientServerPort(cfgPath)
		if !ok || port != 9199 || path != cfgPath {
			t.Errorf("got (%d, %q, %v), want (9199, %q, true)", port, path, ok, cfgPath)
		}
	})

	t.Run("WORKSPACE env var derives the config path", func(t *testing.T) {
		originalWorkspaceEnv := os.Getenv("WORKSPACE")
		defer os.Setenv("WORKSPACE", originalWorkspaceEnv)
		os.Setenv("WORKSPACE", tmpDir)

		port, path, ok := resolveClientServerPort("")
		if !ok || port != 9199 || path != cfgPath {
			t.Errorf("got (%d, %q, %v), want (9199, %q, true)", port, path, ok, cfgPath)
		}
	})

	t.Run("missing config in workspace returns false, not an error", func(t *testing.T) {
		originalWorkspaceEnv := os.Getenv("WORKSPACE")
		defer os.Setenv("WORKSPACE", originalWorkspaceEnv)
		os.Setenv("WORKSPACE", t.TempDir())

		port, path, ok := resolveClientServerPort("")
		if ok {
			t.Errorf("got (%d, %q, %v), want ok=false for a workspace with no hostmcp.yaml", port, path, ok)
		}
	})

	t.Run("nonexistent explicit cfgFile returns false, not an error", func(t *testing.T) {
		port, path, ok := resolveClientServerPort(filepath.Join(tmpDir, "does-not-exist.yaml"))
		if ok {
			t.Errorf("got (%d, %q, %v), want ok=false for a nonexistent config path", port, path, ok)
		}
	})

	// Intentionally not tested here: the bare "$WORKSPACE unset, fixed
	// /workspace fallback" branch (cfgFile == "" && $WORKSPACE == ""),
	// since this test binary itself runs inside a container whose real
	// /workspace/.sandbox/config/hostmcp.yaml exists — asserting on that
	// branch would make the test's outcome depend on this repo's own
	// environment rather than on resolveClientServerPort's logic.
	//
	// ここでは意図的に「$WORKSPACE未設定、固定の/workspaceフォールバック」
	// という素の分岐（cfgFileが空かつ$WORKSPACEも空）はテストしません。
	// このテストバイナリ自体が実際の/workspace/.sandbox/config/hostmcp.yamlを
	// 持つコンテナ内で動くため、この分岐をアサートするとテスト結果が
	// resolveClientServerPortのロジックではなくこのリポジトリ自身の環境に
	// 依存してしまうためです。
}

// TestExtractJSONFromMarkdown tests the extraction of JSON from Markdown code blocks.
// The MCP server wraps JSON responses in Markdown format for AI assistants.
//
// TestExtractJSONFromMarkdownはMarkdownコードブロックからのJSON抽出をテストします。
// MCPサーバーはAIアシスタント向けにJSONレスポンスをMarkdown形式でラップします。
func TestExtractJSONFromMarkdown(t *testing.T) {
	tests := []struct {
		name     string // Test case name / テストケース名
		input    string // Input content / 入力コンテンツ
		expected string // Expected output / 期待される出力
	}{
		{
			name: "standard markdown format",
			input: `Container 'test' details:

` + "```json\n" + `{"Id": "abc123", "Name": "test"}` + "\n```",
			expected: `{"Id": "abc123", "Name": "test"}`,
		},
		{
			name: "multiline JSON in markdown",
			input: `Container 'test' details:

` + "```json\n" + `{
  "Id": "abc123",
  "Name": "test"
}` + "\n```",
			expected: `{
  "Id": "abc123",
  "Name": "test"
}`,
		},
		{
			name:     "plain JSON without markdown",
			input:    `{"Id": "abc123", "Name": "test"}`,
			expected: `{"Id": "abc123", "Name": "test"}`,
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name: "markdown without closing marker",
			input: `Container 'test' details:

` + "```json\n" + `{"Id": "abc123"}`,
			expected: `{"Id": "abc123"}`,
		},
		{
			name: "text before and after code block",
			input: `Some header text

` + "```json\n" + `{"key": "value"}` + "\n```" + `

Some footer text`,
			expected: `{"key": "value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSONFromMarkdown(tt.input)
			if result != tt.expected {
				t.Errorf("extractJSONFromMarkdown() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// Verifies that lifecycle subcommands are registered with correct flags.
// lifecycleサブコマンドが正しいフラグで登録されていることを確認します。
func TestClientLifecycleCommands(t *testing.T) {
	// Restart command
	if clientRestartCmd == nil {
		t.Fatal("clientRestartCmd is nil")
	}
	if clientRestartCmd.Use != "restart CONTAINER" {
		t.Errorf("Expected Use 'restart CONTAINER', got %s", clientRestartCmd.Use)
	}
	flag := clientRestartCmd.Flags().Lookup("timeout")
	if flag == nil {
		t.Fatal("timeout flag not found on clientRestartCmd")
	}
	if flag.Value.Type() != "int" {
		t.Errorf("Expected timeout flag type 'int', got %s", flag.Value.Type())
	}

	// Stop command
	if clientStopCmd == nil {
		t.Fatal("clientStopCmd is nil")
	}
	if clientStopCmd.Use != "stop CONTAINER" {
		t.Errorf("Expected Use 'stop CONTAINER', got %s", clientStopCmd.Use)
	}
	flag = clientStopCmd.Flags().Lookup("timeout")
	if flag == nil {
		t.Fatal("timeout flag not found on clientStopCmd")
	}

	// Start command
	if clientStartCmd == nil {
		t.Fatal("clientStartCmd is nil")
	}
	if clientStartCmd.Use != "start CONTAINER" {
		t.Errorf("Expected Use 'start CONTAINER', got %s", clientStartCmd.Use)
	}
}

// Verifies that host-tools subcommands are registered.
// host-toolsサブコマンドが登録されていることを確認します。
func TestClientHostToolsCommands(t *testing.T) {
	if clientHostToolsCmd == nil {
		t.Fatal("clientHostToolsCmd is nil")
	}
	if clientHostToolsCmd.Use != "host-tools" {
		t.Errorf("Expected Use 'host-tools', got %s", clientHostToolsCmd.Use)
	}

	// Verify sub-subcommands
	// サブサブコマンドを確認
	subCmds := clientHostToolsCmd.Commands()
	subNames := make(map[string]bool)
	for _, cmd := range subCmds {
		use := cmd.Use
		for i, ch := range use {
			if ch == ' ' {
				use = use[:i]
				break
			}
		}
		subNames[use] = true
	}

	for _, expected := range []string{"list", "info", "run"} {
		if !subNames[expected] {
			t.Errorf("Missing host-tools subcommand: %s", expected)
		}
	}
}

// Verifies that host-exec command is registered with correct flags.
// host-execコマンドが正しいフラグで登録されていることを確認します。
func TestClientHostExecCommand(t *testing.T) {
	if clientHostExecCmd == nil {
		t.Fatal("clientHostExecCmd is nil")
	}
	if clientHostExecCmd.Use != "host-exec COMMAND" {
		t.Errorf("Expected Use 'host-exec COMMAND', got %s", clientHostExecCmd.Use)
	}

	flag := clientHostExecCmd.Flags().Lookup("dangerously")
	if flag == nil {
		t.Fatal("dangerously flag not found on clientHostExecCmd")
	}
	if flag.DefValue != "false" {
		t.Errorf("Expected dangerously default 'false', got %s", flag.DefValue)
	}
}
