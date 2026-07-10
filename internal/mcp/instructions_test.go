// Package mcp provides tests for the dynamic `instructions` field returned in
// the MCP `initialize` response, mirroring sandbox-mcp's self-describing pattern.
//
// mcpパッケージは、MCPの`initialize`レスポンスで返される動的な`instructions`
// フィールドのテストを提供します。これはsandbox-mcpの自己記述パターンを模倣しています。
package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YujiSuzuki/hostmcp/internal/config"
	"github.com/YujiSuzuki/hostmcp/internal/hosttools"
	"github.com/YujiSuzuki/hostmcp/internal/security"
)

// TestBuildInstructions_NoHostToolsManager verifies that buildInstructions
// still returns a description of HostMCP's container tools even when no
// host tools manager is configured, without mentioning host tools at all.
//
// TestBuildInstructions_NoHostToolsManagerは、ホストツールマネージャーが
// 設定されていない場合でも、buildInstructionsがHostMCPのコンテナツールの
// 説明を返すこと、かつホストツールについて一切言及しないことを確認します。
func TestBuildInstructions_NoHostToolsManager(t *testing.T) {
	server := NewServer(nil, 8080)
	instructions := server.buildInstructions()

	if !strings.Contains(instructions, "HostMCP") {
		t.Errorf("instructions should describe HostMCP, got: %q", instructions)
	}
	if strings.Contains(instructions, "host tool") {
		t.Errorf("instructions should not mention host tools when no manager is configured, got: %q", instructions)
	}
}

// TestBuildInstructions_NoEnabledTools verifies that when host tools are
// enabled but none are approved yet, buildInstructions points to the
// ai-sandbox generic sample host-tools as a starting point.
//
// TestBuildInstructions_NoEnabledToolsは、ホストツールが有効だがまだ
// 承認済みのものがない場合、buildInstructionsがai-sandboxの汎用サンプル
// host-toolsを出発点として案内することを確認します。
func TestBuildInstructions_NoEnabledTools(t *testing.T) {
	workspaceDir := t.TempDir()
	cfg := &config.HostToolsConfig{
		Enabled:           true,
		ApprovedDir:       t.TempDir(),
		StagingDirs:       []string{"host-tools"},
		AllowedExtensions: []string{".sh"},
		Timeout:           30,
	}
	manager := hosttools.NewManager(cfg, workspaceDir)
	server := NewServer(nil, 8080, WithHostToolsManager(manager))

	instructions := server.buildInstructions()

	if !strings.Contains(instructions, "github.com/YujiSuzuki/ai-sandbox") {
		t.Errorf("instructions should point to the ai-sandbox generic samples, got: %q", instructions)
	}
	if !strings.Contains(instructions, "hostmcp tools sync") {
		t.Errorf("instructions should mention `hostmcp tools sync` in secure mode, got: %q", instructions)
	}
}

// TestBuildInstructions_EnabledAndPendingTools verifies that buildInstructions
// lists both currently-enabled (approved) tools and tools staged but not yet
// approved, distinguishing the two.
//
// TestBuildInstructions_EnabledAndPendingToolsは、buildInstructionsが現在
// 有効な（承認済み）ツールと、ステージング済みだがまだ承認されていない
// ツールの両方を区別してリストすることを確認します。
func TestBuildInstructions_EnabledAndPendingTools(t *testing.T) {
	workspaceDir := t.TempDir()
	approvedBaseDir := t.TempDir()

	// Approved tool
	projectID := hosttools.ProjectID(workspaceDir)
	approvedDir := filepath.Join(approvedBaseDir, projectID)
	os.MkdirAll(approvedDir, 0755)
	os.WriteFile(filepath.Join(approvedDir, "docker-compose-up.sh"),
		[]byte("#!/bin/bash\n# docker-compose-up.sh\n# Start containers\n"), 0755)

	// Staged-but-not-yet-approved tool
	stagingDir := filepath.Join(workspaceDir, "host-tools")
	os.MkdirAll(stagingDir, 0755)
	os.WriteFile(filepath.Join(stagingDir, "docker-compose-up.sh"),
		[]byte("#!/bin/bash\n# docker-compose-up.sh\n# Start containers\n"), 0755)
	os.WriteFile(filepath.Join(stagingDir, "new-tool.sh"),
		[]byte("#!/bin/bash\n# new-tool.sh\n# Brand new\n"), 0755)

	cfg := &config.HostToolsConfig{
		Enabled:           true,
		ApprovedDir:       approvedBaseDir,
		StagingDirs:       []string{"host-tools"},
		AllowedExtensions: []string{".sh"},
		Timeout:           30,
	}
	manager := hosttools.NewManager(cfg, workspaceDir)
	server := NewServer(nil, 8080, WithHostToolsManager(manager))

	instructions := server.buildInstructions()

	if !strings.Contains(instructions, "docker-compose-up.sh: Start containers") {
		t.Errorf("instructions should list the enabled tool with its description, got: %q", instructions)
	}
	if !strings.Contains(instructions, "new-tool.sh (new)") {
		t.Errorf("instructions should list the pending tool marked as new, got: %q", instructions)
	}
	if strings.Contains(instructions, "github.com/YujiSuzuki/ai-sandbox") {
		t.Errorf("instructions should not suggest generic samples once a tool is enabled, got: %q", instructions)
	}
}

// TestBuildInstructions_LegacyModeNoSyncMention verifies that legacy mode
// (no ApprovedDir configured) does not tell the user to run `hostmcp tools sync`,
// since that approval workflow only exists in secure mode.
//
// TestBuildInstructions_LegacyModeNoSyncMentionは、レガシーモード
// （ApprovedDir未設定）では`hostmcp tools sync`の実行を案内しないことを
// 確認します。この承認ワークフローはセキュアモードにのみ存在するためです。
func TestBuildInstructions_LegacyModeNoSyncMention(t *testing.T) {
	workspaceDir := t.TempDir()
	cfg := &config.HostToolsConfig{
		Enabled:           true,
		Directories:       []string{"host-tools"},
		AllowedExtensions: []string{".sh"},
		Timeout:           30,
	}
	manager := hosttools.NewManager(cfg, workspaceDir)
	server := NewServer(nil, 8080, WithHostToolsManager(manager))

	instructions := server.buildInstructions()

	if strings.Contains(instructions, "hostmcp tools sync") {
		t.Errorf("legacy mode instructions should not mention `hostmcp tools sync`, got: %q", instructions)
	}
}

// TestBuildInstructions_UpdatedApprovedToolShowsAsPending_NonDevMode verifies
// that when an already-approved tool's staged copy is changed (not identical
// content), it's reported as "updated, pending re-approval" even though its
// name also appears in the enabled list. In non-dev mode, ListTools() only
// reflects the approved directory, so a name match with a pending item means
// a real pending update — it must not be silently dropped as if it were the
// dev-mode "already shown as enabled" case.
//
// TestBuildInstructions_UpdatedApprovedToolShowsAsPending_NonDevModeは、
// 既に承認済みのツールのstaging側コピーが変更された場合（内容が同一でない）、
// そのツール名がenabledリストにも出現していても「updated, pending
// re-approval」として報告されることを確認します。非devモードでは
// ListTools()は承認済みディレクトリのみを反映するため、pending項目との
// 名前一致は実際に承認待ちの更新を意味し、devモードの「既にenabledとして
// 表示済み」のケースと同様に黙って捨ててはいけません。
func TestBuildInstructions_UpdatedApprovedToolShowsAsPending_NonDevMode(t *testing.T) {
	workspaceDir := t.TempDir()
	approvedBaseDir := t.TempDir()

	projectID := hosttools.ProjectID(workspaceDir)
	approvedDir := filepath.Join(approvedBaseDir, projectID)
	os.MkdirAll(approvedDir, 0755)
	os.WriteFile(filepath.Join(approvedDir, "docker-compose-up.sh"),
		[]byte("#!/bin/bash\n# docker-compose-up.sh\n# Start containers\n"), 0755)

	stagingDir := filepath.Join(workspaceDir, "host-tools")
	os.MkdirAll(stagingDir, 0755)
	// Same name, different (longer) content than the approved copy — a real update.
	os.WriteFile(filepath.Join(stagingDir, "docker-compose-up.sh"),
		[]byte("#!/bin/bash\n# docker-compose-up.sh\n# Start containers (now with --build)\n"), 0755)

	cfg := &config.HostToolsConfig{
		Enabled:           true,
		ApprovedDir:       approvedBaseDir,
		StagingDirs:       []string{"host-tools"},
		AllowedExtensions: []string{".sh"},
		Timeout:           30,
	}
	manager := hosttools.NewManager(cfg, workspaceDir)
	// Deliberately not dev mode (SetDevMode is never called) — this is the
	// normal secure-mode path where the bug applied.
	server := NewServer(nil, 8080, WithHostToolsManager(manager))

	instructions := server.buildInstructions()

	if !strings.Contains(instructions, "docker-compose-up.sh (updated, pending re-approval)") {
		t.Errorf("instructions should surface the pending update for an already-approved tool, got: %q", instructions)
	}
}

// TestBuildInstructions_HostCommandPolicyMentionedIndependently verifies that
// exec_host_command is mentioned whenever hostCommandPolicy is configured,
// even when hostToolsManager is nil/disabled — the two are independent
// config sections (host_tools vs. host_commands in internal/cli/serve.go),
// so a deployment can have one enabled without the other.
//
// TestBuildInstructions_HostCommandPolicyMentionedIndependentlyは、
// hostToolsManagerがnil/無効な場合でも、hostCommandPolicyが設定されていれば
// exec_host_commandが案内されることを確認します。両者は独立した設定
// セクション（internal/cli/serve.goのhost_tools と host_commands）なので、
// 片方だけが有効な構成があり得ます。
func TestBuildInstructions_HostCommandPolicyMentionedIndependently(t *testing.T) {
	policy := security.NewHostCommandPolicy(&config.HostCommandsConfig{
		Enabled:   true,
		Whitelist: map[string][]string{"git": {"status"}},
	})
	server := NewServer(nil, 8080, WithHostCommandPolicy(policy, t.TempDir(), 0))

	instructions := server.buildInstructions()

	if !strings.Contains(instructions, "exec_host_command") {
		t.Errorf("instructions should mention exec_host_command when hostCommandPolicy is set (independent of hostToolsManager), got: %q", instructions)
	}
}

// TestBuildInstructions_DetectionFailureNotReportedAsUnconfigured verifies
// that when PendingApproval fails (e.g. an unreadable approved file), the
// instructions say detection failed rather than falsely claiming no host
// tools are configured yet.
//
// TestBuildInstructions_DetectionFailureNotReportedAsUnconfiguredは、
// PendingApprovalが失敗した場合（例: 読み取り不可能な承認済みファイル）、
// instructionsが「ホストツール未設定」と誤って報告せず、検出失敗である旨を
// 報告することを確認します。
func TestBuildInstructions_DetectionFailureNotReportedAsUnconfigured(t *testing.T) {
	workspaceDir := t.TempDir()
	approvedBaseDir := t.TempDir()

	projectID := hosttools.ProjectID(workspaceDir)
	approvedDir := filepath.Join(approvedBaseDir, projectID)
	os.MkdirAll(approvedDir, 0755)

	stagingDir := filepath.Join(workspaceDir, "host-tools")
	os.MkdirAll(stagingDir, 0755)

	content := []byte("#!/bin/bash\n# broken.sh\n# Unreadable approved copy\n")
	os.WriteFile(filepath.Join(stagingDir, "broken.sh"), content, 0755)

	// Same size as the staging file, so compareFiles proceeds past the
	// size check into fileHash(), where the permission error surfaces.
	approvedPath := filepath.Join(approvedDir, "broken.sh")
	os.WriteFile(approvedPath, content, 0755)
	if err := os.Chmod(approvedPath, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(approvedPath, 0755) // allow TempDir cleanup

	cfg := &config.HostToolsConfig{
		Enabled:           true,
		ApprovedDir:       approvedBaseDir,
		StagingDirs:       []string{"host-tools"},
		AllowedExtensions: []string{".sh"},
		Timeout:           30,
	}
	manager := hosttools.NewManager(cfg, workspaceDir)
	server := NewServer(nil, 8080, WithHostToolsManager(manager))

	instructions := server.buildInstructions()

	if strings.Contains(instructions, "No host tools are enabled yet") {
		t.Errorf("a detection error should not be reported as \"no host tools configured\", got: %q", instructions)
	}
	if !strings.Contains(instructions, "Could not fully determine host tool status") {
		t.Errorf("instructions should report the detection failure, got: %q", instructions)
	}
}
