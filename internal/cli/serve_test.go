// serve_test.go contains unit tests for the serve command functionality.
// It tests the applyWorkspaceOverrides, applyAllowExecFlags, applyDangerouslyFlags,
// writeSponsorMessage, and resolveConfigFile functions.
//
// serve_test.goはserveコマンドの機能のユニットテストを含みます。
// applyWorkspaceOverrides、applyAllowExecFlags、applyDangerouslyFlags、
// writeSponsorMessage、およびresolveConfigFile関数をテストします。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YujiSuzuki/hostmcp/internal/config"
)

// TestApplyWorkspaceOverrides tests all three transformation steps of applyWorkspaceOverrides.
// The function must: (1) apply --workspace flag, (2) resolve HostAccess.WorkspaceRoot to
// absolute and propagate to AutoImport, (3) resolve a remaining relative AutoImport.WorkspaceRoot.
//
// TestApplyWorkspaceOverridesはapplyWorkspaceOverridesの3つの変換ステップをテストします。
// (1) --workspaceフラグの適用、(2) HostAccess.WorkspaceRootの絶対パス解決とAutoImportへの伝播、
// (3) 残った相対パスのAutoImport.WorkspaceRootの解決。
func TestApplyWorkspaceOverrides(t *testing.T) {
	tests := []struct {
		name          string // Test case name / テストケース名
		flagWorkspace string // Value of --workspace flag / --workspaceフラグの値
		initial       func() *config.Config
		wantErr       bool
		validate      func(*testing.T, *config.Config)
	}{
		{
			// --workspace absolute path sets both fields directly.
			// --workspace に絶対パスを指定すると両フィールドに直接セットされる。
			name:          "absolute flagWorkspace sets both fields",
			flagWorkspace: "/abs/workspace",
			initial: func() *config.Config {
				return &config.Config{}
			},
			validate: func(t *testing.T, cfg *config.Config) {
				if cfg.HostAccess.WorkspaceRoot != "/abs/workspace" {
					t.Errorf("HostAccess.WorkspaceRoot = %q, want /abs/workspace", cfg.HostAccess.WorkspaceRoot)
				}
				if cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot != "/abs/workspace" {
					t.Errorf("AutoImport.WorkspaceRoot = %q, want /abs/workspace", cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot)
				}
			},
		},
		{
			// --workspace relative path is resolved to absolute.
			// --workspace に相対パスを指定すると絶対パスに解決される。
			name:          "relative flagWorkspace is resolved to absolute",
			flagWorkspace: "relative/path",
			initial: func() *config.Config {
				return &config.Config{}
			},
			validate: func(t *testing.T, cfg *config.Config) {
				if !filepath.IsAbs(cfg.HostAccess.WorkspaceRoot) {
					t.Errorf("HostAccess.WorkspaceRoot %q is not absolute", cfg.HostAccess.WorkspaceRoot)
				}
				if !filepath.IsAbs(cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot) {
					t.Errorf("AutoImport.WorkspaceRoot %q is not absolute", cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot)
				}
				if !strings.HasSuffix(cfg.HostAccess.WorkspaceRoot, "relative/path") {
					t.Errorf("HostAccess.WorkspaceRoot %q should end with relative/path", cfg.HostAccess.WorkspaceRoot)
				}
				if cfg.HostAccess.WorkspaceRoot != cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot {
					t.Errorf("fields should be equal: %q vs %q",
						cfg.HostAccess.WorkspaceRoot, cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot)
				}
			},
		},
		{
			// Config workspace_root is resolved to absolute and propagated to AutoImport.
			// 設定ファイルのworkspace_rootが絶対パスに解決されAutoImportにも伝播する。
			name:          "config workspace_root resolved and propagated to AutoImport",
			flagWorkspace: "",
			initial: func() *config.Config {
				return &config.Config{
					HostAccess: config.HostAccessConfig{
						WorkspaceRoot: "/config/workspace",
					},
				}
			},
			validate: func(t *testing.T, cfg *config.Config) {
				if cfg.HostAccess.WorkspaceRoot != "/config/workspace" {
					t.Errorf("HostAccess.WorkspaceRoot = %q, want /config/workspace", cfg.HostAccess.WorkspaceRoot)
				}
				if cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot != "/config/workspace" {
					t.Errorf("AutoImport.WorkspaceRoot = %q, want /config/workspace", cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot)
				}
			},
		},
		{
			// AutoImport default "." is resolved to absolute CWD (no HostAccess.WorkspaceRoot set).
			// AutoImportのデフォルト"."がCWDの絶対パスに解決される（HostAccess.WorkspaceRootなし）。
			name:          "AutoImport dot resolved to absolute CWD",
			flagWorkspace: "",
			initial: func() *config.Config {
				return &config.Config{
					Security: config.SecurityConfig{
						BlockedPaths: config.BlockedPathsConfig{
							AutoImport: config.AutoImportConfig{
								WorkspaceRoot: ".",
							},
						},
					},
				}
			},
			validate: func(t *testing.T, cfg *config.Config) {
				if !filepath.IsAbs(cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot) {
					t.Errorf("AutoImport.WorkspaceRoot %q should be absolute after resolving dot",
						cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot)
				}
				// HostAccess.WorkspaceRoot was not set, so it stays empty.
				// HostAccess.WorkspaceRootは設定されていないので空のまま。
				if cfg.HostAccess.WorkspaceRoot != "" {
					t.Errorf("HostAccess.WorkspaceRoot = %q, want empty", cfg.HostAccess.WorkspaceRoot)
				}
			},
		},
		{
			// Absolute AutoImport.WorkspaceRoot is preserved when no HostAccess.WorkspaceRoot is set.
			// HostAccess.WorkspaceRootが未設定の場合、絶対パスのAutoImport.WorkspaceRootは保持される。
			name:          "absolute AutoImport.WorkspaceRoot preserved without HostAccess",
			flagWorkspace: "",
			initial: func() *config.Config {
				return &config.Config{
					Security: config.SecurityConfig{
						BlockedPaths: config.BlockedPathsConfig{
							AutoImport: config.AutoImportConfig{
								WorkspaceRoot: "/explicit/auto-import",
							},
						},
					},
				}
			},
			validate: func(t *testing.T, cfg *config.Config) {
				if cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot != "/explicit/auto-import" {
					t.Errorf("AutoImport.WorkspaceRoot = %q, want /explicit/auto-import",
						cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot)
				}
				if cfg.HostAccess.WorkspaceRoot != "" {
					t.Errorf("HostAccess.WorkspaceRoot = %q, want empty", cfg.HostAccess.WorkspaceRoot)
				}
			},
		},
		{
			// --workspace flag takes precedence over config file workspace_root.
			// --workspaceフラグは設定ファイルのworkspace_rootより優先される。
			name:          "flagWorkspace takes precedence over config workspace_root",
			flagWorkspace: "/flag/workspace",
			initial: func() *config.Config {
				return &config.Config{
					HostAccess: config.HostAccessConfig{
						WorkspaceRoot: "/config/workspace",
					},
				}
			},
			validate: func(t *testing.T, cfg *config.Config) {
				if cfg.HostAccess.WorkspaceRoot != "/flag/workspace" {
					t.Errorf("HostAccess.WorkspaceRoot = %q, want /flag/workspace", cfg.HostAccess.WorkspaceRoot)
				}
				if cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot != "/flag/workspace" {
					t.Errorf("AutoImport.WorkspaceRoot = %q, want /flag/workspace",
						cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot)
				}
			},
		},
		{
			// HostAccess.WorkspaceRoot overrides AutoImport.WorkspaceRoot set separately in config.
			// HostAccess.WorkspaceRootは設定で個別に指定されたAutoImport.WorkspaceRootを上書きする。
			name:          "HostAccess.WorkspaceRoot overrides AutoImport.WorkspaceRoot from config",
			flagWorkspace: "",
			initial: func() *config.Config {
				return &config.Config{
					HostAccess: config.HostAccessConfig{
						WorkspaceRoot: "/host/workspace",
					},
					Security: config.SecurityConfig{
						BlockedPaths: config.BlockedPathsConfig{
							AutoImport: config.AutoImportConfig{
								WorkspaceRoot: "/different/auto-import",
							},
						},
					},
				}
			},
			validate: func(t *testing.T, cfg *config.Config) {
				if cfg.HostAccess.WorkspaceRoot != "/host/workspace" {
					t.Errorf("HostAccess.WorkspaceRoot = %q, want /host/workspace", cfg.HostAccess.WorkspaceRoot)
				}
				if cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot != "/host/workspace" {
					t.Errorf("AutoImport.WorkspaceRoot = %q, want /host/workspace (overridden by HostAccess)",
						cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.initial()
			err := applyWorkspaceOverrides(cfg, tt.flagWorkspace)
			if (err != nil) != tt.wantErr {
				t.Fatalf("applyWorkspaceOverrides() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				tt.validate(t, cfg)
			}
		})
	}
}

// TestResolveConfigFile tests the resolveConfigFile helper for all key scenarios.
//
// TestResolveConfigFileはresolveConfigFileヘルパーの主要シナリオをテストします。
func TestResolveConfigFile(t *testing.T) {
	t.Run("neither flag given returns error", func(t *testing.T) {
		_, err := resolveConfigFile("", "")
		if err == nil {
			t.Fatal("expected error when neither --config nor --workspace given")
		}
		if !strings.Contains(err.Error(), "--config") || !strings.Contains(err.Error(), "--workspace") {
			t.Errorf("error should mention both flags, got: %v", err)
		}
	})

	t.Run("--config given returns it as-is", func(t *testing.T) {
		got, err := resolveConfigFile("/some/custom/hostmcp.yaml", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "/some/custom/hostmcp.yaml" {
			t.Errorf("expected /some/custom/hostmcp.yaml, got %s", got)
		}
	})

	t.Run("--workspace with existing config returns derived path", func(t *testing.T) {
		tmpDir := t.TempDir()
		configDir := filepath.Join(tmpDir, ".sandbox", "config")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(configDir, "hostmcp.yaml")
		if err := os.WriteFile(configPath, []byte("server:\n  port: 8080\n"), 0644); err != nil {
			t.Fatal(err)
		}

		got, err := resolveConfigFile("", tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != configPath {
			t.Errorf("expected %s, got %s", configPath, got)
		}
	})

	t.Run("--workspace with missing config returns error mentioning hostmcp init", func(t *testing.T) {
		emptyDir := t.TempDir()
		_, err := resolveConfigFile("", emptyDir)
		if err == nil {
			t.Fatal("expected error when config file not found")
		}
		if !strings.Contains(err.Error(), "hostmcp init") {
			t.Errorf("expected error to mention 'hostmcp init', got: %v", err)
		}
	})
}

// TestApplyAllowExecFlags tests the applyAllowExecFlags function with various inputs.
// It uses table-driven tests to cover different scenarios.
//
// TestApplyAllowExecFlagsは様々な入力でapplyAllowExecFlags関数をテストします。
// 異なるシナリオをカバーするためにテーブル駆動テストを使用します。
func TestApplyAllowExecFlags(t *testing.T) {
	// Define test cases as a table.
	// テストケースをテーブルとして定義します。
	tests := []struct {
		name          string         // Test case name / テストケース名
		flags         []string       // Input flags / 入力フラグ
		initialConfig *config.Config // Initial configuration / 初期設定
		wantErr       bool           // Whether error is expected / エラーが期待されるかどうか
		validate      func(*testing.T, *config.Config)
	}{
		{
			// Test case: empty flags should not modify config.
			// テストケース：空のフラグは設定を変更しないこと。
			name:  "empty flags",
			flags: []string{},
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecWhitelist: nil,
				},
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *config.Config) {
				// ExecWhitelist should remain nil when no flags provided.
				// フラグが提供されていない場合、ExecWhitelistはnilのままであるべきです。
				if cfg.Security.ExecWhitelist != nil {
					t.Error("Expected ExecWhitelist to remain nil")
				}
			},
		},
		{
			// Test case: single valid flag should create whitelist entry.
			// テストケース：単一の有効なフラグがホワイトリストエントリを作成すること。
			name:  "single valid flag",
			flags: []string{"mycontainer:npm test"},
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecWhitelist: nil,
				},
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *config.Config) {
				// Verify ExecWhitelist was initialized.
				// ExecWhitelistが初期化されたことを確認します。
				if cfg.Security.ExecWhitelist == nil {
					t.Fatal("Expected ExecWhitelist to be initialized")
				}
				// Verify the container entry exists.
				// コンテナエントリが存在することを確認します。
				commands, ok := cfg.Security.ExecWhitelist["mycontainer"]
				if !ok {
					t.Fatal("Expected mycontainer in ExecWhitelist")
				}
				// Verify the command was added.
				// コマンドが追加されたことを確認します。
				if len(commands) != 1 || commands[0] != "npm test" {
					t.Errorf("Expected [npm test], got %v", commands)
				}
			},
		},
		{
			// Test case: multiple flags for same container should append.
			// テストケース：同じコンテナへの複数のフラグが追加されること。
			name:  "multiple flags for same container",
			flags: []string{"mycontainer:npm test", "mycontainer:npm install"},
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecWhitelist: nil,
				},
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *config.Config) {
				// Verify both commands were added.
				// 両方のコマンドが追加されたことを確認します。
				commands := cfg.Security.ExecWhitelist["mycontainer"]
				if len(commands) != 2 {
					t.Fatalf("Expected 2 commands, got %d", len(commands))
				}
				if commands[0] != "npm test" || commands[1] != "npm install" {
					t.Errorf("Expected [npm test, npm install], got %v", commands)
				}
			},
		},
		{
			// Test case: flags for different containers should create separate entries.
			// テストケース：異なるコンテナへのフラグが別々のエントリを作成すること。
			name:  "multiple flags for different containers",
			flags: []string{"container1:cmd1", "container2:cmd2"},
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecWhitelist: nil,
				},
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *config.Config) {
				// Verify both containers were added.
				// 両方のコンテナが追加されたことを確認します。
				if len(cfg.Security.ExecWhitelist) != 2 {
					t.Fatalf("Expected 2 containers, got %d", len(cfg.Security.ExecWhitelist))
				}
				if cfg.Security.ExecWhitelist["container1"][0] != "cmd1" {
					t.Error("container1 command mismatch")
				}
				if cfg.Security.ExecWhitelist["container2"][0] != "cmd2" {
					t.Error("container2 command mismatch")
				}
			},
		},
		{
			// Test case: command containing colon should be parsed correctly.
			// テストケース：コロンを含むコマンドが正しく解析されること。
			name:  "command with colon",
			flags: []string{"mycontainer:echo foo:bar"},
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecWhitelist: nil,
				},
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *config.Config) {
				// Verify the command with colon was preserved.
				// Only the first colon should be used as separator.
				//
				// コロンを含むコマンドが保持されたことを確認します。
				// 最初のコロンのみがセパレータとして使用されるべきです。
				commands := cfg.Security.ExecWhitelist["mycontainer"]
				if commands[0] != "echo foo:bar" {
					t.Errorf("Expected 'echo foo:bar', got '%s'", commands[0])
				}
			},
		},
		{
			// Test case: invalid format without colon should return error.
			// テストケース：コロンなしの無効なフォーマットがエラーを返すこと。
			name:  "invalid format - no colon",
			flags: []string{"mycontainer-npm-test"},
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecWhitelist: nil,
				},
			},
			wantErr: true,
			validate: func(t *testing.T, cfg *config.Config) {
				// Config should not be modified on error.
				// エラー時に設定は変更されないべきです。
			},
		},
		{
			// Test case: only colon should return error.
			// テストケース：コロンのみがエラーを返すこと。
			name:  "invalid format - only colon",
			flags: []string{":"},
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecWhitelist: nil,
				},
			},
			wantErr: true,
			validate: func(t *testing.T, cfg *config.Config) {
				// Config should not be modified on error.
				// エラー時に設定は変更されないべきです。
			},
		},
		{
			// Test case: new flag should append to existing whitelist.
			// テストケース：新しいフラグが既存のホワイトリストに追加されること。
			name:  "append to existing whitelist",
			flags: []string{"mycontainer:new command"},
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecWhitelist: map[string][]string{
						"mycontainer": {"existing command"},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *config.Config) {
				// Verify both existing and new commands are present.
				// 既存のコマンドと新しいコマンドの両方が存在することを確認します。
				commands := cfg.Security.ExecWhitelist["mycontainer"]
				if len(commands) != 2 {
					t.Fatalf("Expected 2 commands, got %d", len(commands))
				}
				if commands[0] != "existing command" || commands[1] != "new command" {
					t.Errorf("Expected [existing command, new command], got %v", commands)
				}
			},
		},
	}

	// Run each test case.
	// 各テストケースを実行します。
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function under test.
			// テスト対象の関数を呼び出します。
			err := applyAllowExecFlags(tt.initialConfig, tt.flags)

			// Check error expectation.
			// エラーの期待を確認します。
			if (err != nil) != tt.wantErr {
				t.Errorf("applyAllowExecFlags() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Run validation if no error expected.
			// エラーが期待されない場合は検証を実行します。
			if !tt.wantErr {
				tt.validate(t, tt.initialConfig)
			}
		})
	}
}

// TestApplyDangerouslyFlags tests the applyDangerouslyFlags function with various inputs.
// It uses table-driven tests to cover different scenarios including:
// - Empty flags (no changes)
// - --dangerously-all flag (enables for all containers, merges with existing)
// - --dangerously=container flag (clears existing config, enables only for specified)
// - Error when both flags are used together
//
// TestApplyDangerouslyFlagsは様々な入力でapplyDangerouslyFlags関数をテストします。
// 以下のシナリオをカバーするテーブル駆動テストを使用します：
// - 空のフラグ（変更なし）
// - --dangerously-allフラグ（全コンテナに有効化、既存設定とマージ）
// - --dangerously=containerフラグ（既存設定をクリア、指定コンテナのみ有効化）
// - 両方のフラグを同時に使用した場合のエラー
func TestApplyDangerouslyFlags(t *testing.T) {
	tests := []struct {
		name           string         // Test case name / テストケース名
		dangerously    string         // --dangerously flag value / --dangerouslyフラグの値
		dangerouslyAll bool           // --dangerously-all flag value / --dangerously-allフラグの値
		initialConfig  *config.Config // Initial configuration / 初期設定
		wantErr        bool           // Whether error is expected / エラーが期待されるかどうか
		validate       func(*testing.T, *config.Config)
	}{
		{
			// Test case: no flags should not modify config.
			// テストケース：フラグなしの場合は設定を変更しない。
			name:           "no flags",
			dangerously:    "",
			dangerouslyAll: false,
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecDangerously: config.ExecDangerouslyConfig{
						Enabled: false,
						Commands: map[string][]string{
							"existing": {"tail"},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *config.Config) {
				// ExecDangerously should remain unchanged.
				// ExecDangerouslyは変更されないべき。
				if cfg.Security.ExecDangerously.Enabled {
					t.Error("Expected Enabled to remain false")
				}
				if _, ok := cfg.Security.ExecDangerously.Commands["existing"]; !ok {
					t.Error("Expected existing commands to remain")
				}
			},
		},
		{
			// Test case: --dangerously-all should enable for all containers.
			// テストケース：--dangerously-allは全コンテナに対して有効化。
			name:           "dangerously-all flag",
			dangerously:    "",
			dangerouslyAll: true,
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecDangerously: config.ExecDangerouslyConfig{
						Enabled:  false,
						Commands: nil,
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *config.Config) {
				// Dangerous mode should be enabled.
				// 危険モードが有効化されているべき。
				if !cfg.Security.ExecDangerously.Enabled {
					t.Error("Expected Enabled to be true")
				}
				// Global commands should be set.
				// グローバルコマンドが設定されているべき。
				if cmds, ok := cfg.Security.ExecDangerously.Commands["*"]; !ok {
					t.Error("Expected * (global) commands to be set")
				} else if len(cmds) == 0 {
					t.Error("Expected default commands to be added")
				}
			},
		},
		{
			// Test case: --dangerously-all should merge with existing config.
			// テストケース：--dangerously-allは既存設定とマージ。
			name:           "dangerously-all merges with existing",
			dangerously:    "",
			dangerouslyAll: true,
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecDangerously: config.ExecDangerouslyConfig{
						Enabled: false,
						Commands: map[string][]string{
							"existing-container": {"custom-cmd"},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *config.Config) {
				// Existing commands should be preserved.
				// 既存のコマンドが保持されているべき。
				if _, ok := cfg.Security.ExecDangerously.Commands["existing-container"]; !ok {
					t.Error("Expected existing-container commands to be preserved")
				}
				// Global commands should also be set.
				// グローバルコマンドも設定されているべき。
				if _, ok := cfg.Security.ExecDangerously.Commands["*"]; !ok {
					t.Error("Expected * (global) commands to be set")
				}
			},
		},
		{
			// Test case: --dangerously=container should clear existing and enable only for specified.
			// テストケース：--dangerously=containerは既存をクリアし指定コンテナのみ有効化。
			name:           "dangerously clears existing config",
			dangerously:    "securenote-web",
			dangerouslyAll: false,
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecDangerously: config.ExecDangerouslyConfig{
						Enabled: false,
						Commands: map[string][]string{
							"securenote-api": {"tail", "cat"},
							"*":              {"ls"},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *config.Config) {
				// Dangerous mode should be enabled.
				// 危険モードが有効化されているべき。
				if !cfg.Security.ExecDangerously.Enabled {
					t.Error("Expected Enabled to be true")
				}
				// Only specified container should have commands.
				// 指定されたコンテナのみがコマンドを持つべき。
				if _, ok := cfg.Security.ExecDangerously.Commands["securenote-web"]; !ok {
					t.Error("Expected securenote-web to have commands")
				}
				// Existing config should be cleared.
				// 既存の設定はクリアされているべき。
				if _, ok := cfg.Security.ExecDangerously.Commands["securenote-api"]; ok {
					t.Error("Expected securenote-api to be cleared")
				}
				if _, ok := cfg.Security.ExecDangerously.Commands["*"]; ok {
					t.Error("Expected * (global) to be cleared")
				}
			},
		},
		{
			// Test case: --dangerously with multiple containers.
			// テストケース：複数コンテナを指定した--dangerously。
			name:           "dangerously with multiple containers",
			dangerously:    "container1,container2",
			dangerouslyAll: false,
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecDangerously: config.ExecDangerouslyConfig{
						Enabled:  false,
						Commands: nil,
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *config.Config) {
				// Both containers should have commands.
				// 両方のコンテナがコマンドを持つべき。
				if _, ok := cfg.Security.ExecDangerously.Commands["container1"]; !ok {
					t.Error("Expected container1 to have commands")
				}
				if _, ok := cfg.Security.ExecDangerously.Commands["container2"]; !ok {
					t.Error("Expected container2 to have commands")
				}
				// Should only have 2 entries.
				// 2つのエントリのみを持つべき。
				if len(cfg.Security.ExecDangerously.Commands) != 2 {
					t.Errorf("Expected 2 entries, got %d", len(cfg.Security.ExecDangerously.Commands))
				}
			},
		},
		{
			// Test case: both flags should return error.
			// テストケース：両方のフラグを指定するとエラー。
			name:           "both flags error",
			dangerously:    "container1",
			dangerouslyAll: true,
			initialConfig: &config.Config{
				Security: config.SecurityConfig{
					ExecDangerously: config.ExecDangerouslyConfig{
						Enabled:  false,
						Commands: nil,
					},
				},
			},
			wantErr: true,
			validate: func(t *testing.T, cfg *config.Config) {
				// Config should not be modified on error.
				// エラー時に設定は変更されないべき。
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := applyDangerouslyFlags(tt.initialConfig, tt.dangerously, tt.dangerouslyAll)

			if (err != nil) != tt.wantErr {
				t.Errorf("applyDangerouslyFlags() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				tt.validate(t, tt.initialConfig)
			}
		})
	}
}

// TestWriteBanner tests the writeBanner function.
// It verifies that the ASCII art banner contains the expected text.
//
// TestWriteBannerはwriteBanner関数をテストします。
// ASCIIアートバナーに期待されるテキストが含まれていることを検証します。
func TestWriteBanner(t *testing.T) {
	var buf bytes.Buffer
	writeBanner(&buf)
	output := buf.String()

	// Verify banner contains key elements.
	// バナーにキー要素が含まれていることを確認します。
	if !strings.Contains(output, "Sandbox") {
		t.Error("Expected banner to contain 'Sandbox' in ASCII art")
	}
	if !strings.Contains(output, "HostMCP") {
		t.Error("Expected banner to contain 'HostMCP'")
	}
	if !strings.Contains(output, "SandboxMCP") {
		t.Error("Expected banner to contain 'SandboxMCP'")
	}
}

// TestWriteSponsorMessage tests the writeSponsorMessage function.
// It verifies sponsor message display behavior based on the --no-thanks flag
// and LANG environment variable.
//
// TestWriteSponsorMessageはwriteSponsorMessage関数をテストします。
// --no-thanksフラグとLANG環境変数に基づくスポンサーメッセージ表示動作を検証します。
func TestWriteSponsorMessage(t *testing.T) {
	tests := []struct {
		name       string // Test case name / テストケース名
		noThanks   bool   // --no-thanks flag value / --no-thanksフラグの値
		lang       string // LANG environment variable / LANG環境変数
		lcAll      string // LC_ALL environment variable / LC_ALL環境変数
		wantOutput bool   // Whether output is expected / 出力が期待されるか
		wantText   string // Expected text in output / 出力に含まれるべきテキスト
	}{
		{
			name:       "default shows English message",
			noThanks:   false,
			lang:       "",
			lcAll:      "",
			wantOutput: true,
			wantText:   "Support this project",
		},
		{
			name:       "no-thanks suppresses message",
			noThanks:   true,
			lang:       "",
			lcAll:      "",
			wantOutput: false,
		},
		{
			name:       "Japanese locale shows Japanese message",
			noThanks:   false,
			lang:       "ja_JP.UTF-8",
			lcAll:      "",
			wantOutput: true,
			wantText:   "このプロジェクトを応援",
		},
		{
			name:       "English locale shows English message",
			noThanks:   false,
			lang:       "en_US.UTF-8",
			lcAll:      "",
			wantOutput: true,
			wantText:   "Support this project",
		},
		{
			// LC_ALL takes precedence: when LC_ALL is set, it is used regardless of LANG.
			// LC_ALLが優先される: LC_ALLが設定されている場合、LANGに関わらずLC_ALLが使用される。
			name:       "LC_ALL takes precedence when LANG is empty",
			noThanks:   false,
			lang:       "",
			lcAll:      "ja_JP.UTF-8",
			wantOutput: true,
			wantText:   "このプロジェクトを応援",
		},
		{
			// LC_ALL overrides LANG per POSIX precedence.
			// POSIX優先順位に従い、LC_ALLがLANGを上書きすること。
			name:       "LC_ALL overrides LANG",
			noThanks:   false,
			lang:       "en_US.UTF-8",
			lcAll:      "ja_JP.UTF-8",
			wantOutput: true,
			wantText:   "このプロジェクトを応援",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore flag state.
			// フラグの状態を保存して復元します。
			origNoThanks := flagNoThanks
			defer func() { flagNoThanks = origNoThanks }()
			flagNoThanks = tt.noThanks

			// Set locale environment variables.
			// ロケール環境変数を設定します。
			t.Setenv("LANG", tt.lang)
			t.Setenv("LC_ALL", tt.lcAll)

			var buf bytes.Buffer
			wrote := writeSponsorMessage(&buf)
			output := buf.String()

			if tt.wantOutput {
				if !wrote {
					t.Error("Expected writeSponsorMessage to return true")
				}
				if !strings.Contains(output, tt.wantText) {
					t.Errorf("Expected output to contain %q, got %q", tt.wantText, output)
				}
				if !strings.Contains(output, "github.com/sponsors/YujiSuzuki") {
					t.Error("Expected output to contain sponsor URL")
				}
				if !strings.Contains(output, "--no-thanks") {
					t.Error("Expected output to contain --no-thanks hint")
				}
			} else {
				if wrote {
					t.Error("Expected writeSponsorMessage to return false")
				}
				if output != "" {
					t.Errorf("Expected no output, got %q", output)
				}
			}
		})
	}
}
