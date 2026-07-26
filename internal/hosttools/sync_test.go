package hosttools

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YujiSuzuki/hostmcp/internal/config"
)

// TestProjectID verifies that project IDs are generated correctly with proper prefix and length.
// Tests edge cases like special characters and root path.
//
// TestProjectIDはプロジェクトIDが適切な接頭辞と長さで生成されることを確認します。
// 特殊文字やルートパスなどのエッジケースをテストします。
func TestProjectID(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantPrefix string
		wantLen   int // minimum length
	}{
		{
			name:       "simple path",
			path:       "/home/user/my-project",
			wantPrefix: "my-project-",
			wantLen:    19, // "my-project-" + 8 hex chars
		},
		{
			name:       "path with special chars",
			path:       "/home/user/my project (2)",
			wantPrefix: "myproject2-",
			wantLen:    19,
		},
		{
			name:       "root path",
			path:       "/",
			wantPrefix: "", // sanitized "/" base is empty, falls back to "project"
			wantLen:    16, // "project-" + 8 hex chars
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := ProjectID(tt.path)
			if tt.wantPrefix != "" && !strings.HasPrefix(id, tt.wantPrefix) {
				t.Errorf("ProjectID(%q) = %q, want prefix %q", tt.path, id, tt.wantPrefix)
			}
			if len(id) < tt.wantLen {
				t.Errorf("ProjectID(%q) = %q (len %d), want len >= %d", tt.path, id, len(id), tt.wantLen)
			}
		})
	}
}

// TestProjectID_Deterministic verifies that ProjectID returns the same ID for identical paths.
//
// TestProjectID_Deterministicは同じパスに対して同じIDを返すことを確認します。
func TestProjectID_Deterministic(t *testing.T) {
	id1 := ProjectID("/home/user/workspace")
	id2 := ProjectID("/home/user/workspace")
	if id1 != id2 {
		t.Errorf("ProjectID should be deterministic: %q != %q", id1, id2)
	}
}

// TestProjectID_DifferentPaths verifies that different paths produce different project IDs.
//
// TestProjectID_DifferentPathsは異なるパスから異なるプロジェクトIDが生成されることを確認します。
func TestProjectID_DifferentPaths(t *testing.T) {
	id1 := ProjectID("/home/user/project-a")
	id2 := ProjectID("/home/user/project-b")
	if id1 == id2 {
		t.Errorf("Different paths should produce different IDs: both = %q", id1)
	}
}

// TestResolveApprovedDir verifies that approved directory paths are resolved correctly.
// Tests tilde expansion, absolute paths, and relative paths.
//
// TestResolveApprovedDirは承認済みディレクトリのパスが正しく解決されることを確認します。
// チルダ展開、絶対パス、相対パスをテストします。
func TestResolveApprovedDir(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "tilde expansion",
			input:   "~/.hostmcp/host-tools",
			wantErr: false,
		},
		{
			name:    "absolute path",
			input:   "/tmp/approved",
			wantErr: false,
		},
		{
			name:    "relative path",
			input:   "approved-tools",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolveApprovedDir(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveApprovedDir(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if !filepath.IsAbs(result) {
					t.Errorf("ResolveApprovedDir(%q) = %q, want absolute path", tt.input, result)
				}
				if tt.input == "~/.hostmcp/host-tools" {
					home, _ := os.UserHomeDir()
					expected := filepath.Join(home, ".hostmcp/host-tools")
					if result != expected {
						t.Errorf("ResolveApprovedDir(%q) = %q, want %q", tt.input, result, expected)
					}
				}
			}
		})
	}
}

// TestCompareFiles verifies file comparison logic for sync status detection.
// Tests new files, unchanged files, and updated files with different content or size.
//
// TestCompareFilesは同期ステータス検出のためのファイル比較ロジックを確認します。
// 新規ファイル、未変更ファイル、内容やサイズが異なる更新ファイルをテストします。
func TestCompareFiles(t *testing.T) {
	dir := t.TempDir()

	// Create staging file
	stagingPath := filepath.Join(dir, "staging.sh")
	os.WriteFile(stagingPath, []byte("#!/bin/bash\necho hello\n"), 0755)

	t.Run("new file (approved doesn't exist)", func(t *testing.T) {
		approvedPath := filepath.Join(dir, "nonexistent.sh")
		status, err := compareFiles(stagingPath, approvedPath)
		if err != nil {
			t.Fatalf("compareFiles error: %v", err)
		}
		if status != SyncNew {
			t.Errorf("status = %d, want SyncNew (%d)", status, SyncNew)
		}
	})

	t.Run("unchanged (identical content)", func(t *testing.T) {
		approvedPath := filepath.Join(dir, "identical.sh")
		os.WriteFile(approvedPath, []byte("#!/bin/bash\necho hello\n"), 0755)
		status, err := compareFiles(stagingPath, approvedPath)
		if err != nil {
			t.Fatalf("compareFiles error: %v", err)
		}
		if status != SyncUnchanged {
			t.Errorf("status = %d, want SyncUnchanged (%d)", status, SyncUnchanged)
		}
	})

	t.Run("updated (different content)", func(t *testing.T) {
		approvedPath := filepath.Join(dir, "different.sh")
		os.WriteFile(approvedPath, []byte("#!/bin/bash\necho world\n"), 0755)
		status, err := compareFiles(stagingPath, approvedPath)
		if err != nil {
			t.Fatalf("compareFiles error: %v", err)
		}
		if status != SyncUpdated {
			t.Errorf("status = %d, want SyncUpdated (%d)", status, SyncUpdated)
		}
	})

	t.Run("updated (different size)", func(t *testing.T) {
		approvedPath := filepath.Join(dir, "diffsize.sh")
		os.WriteFile(approvedPath, []byte("#!/bin/bash\n"), 0755)
		status, err := compareFiles(stagingPath, approvedPath)
		if err != nil {
			t.Fatalf("compareFiles error: %v", err)
		}
		if status != SyncUpdated {
			t.Errorf("status = %d, want SyncUpdated (%d)", status, SyncUpdated)
		}
	})
}

// TestCopyFile verifies that files are copied with correct content and permissions.
// Tests that parent directories are created and file mode is preserved.
//
// TestCopyFileはファイルが正しい内容とパーミッションでコピーされることを確認します。
// 親ディレクトリの作成とファイルモードの保持をテストします。
func TestCopyFile(t *testing.T) {
	dir := t.TempDir()

	src := filepath.Join(dir, "src.sh")
	content := []byte("#!/bin/bash\necho test\n")
	os.WriteFile(src, content, 0755)

	dst := filepath.Join(dir, "subdir", "dst.sh")

	err := copyFile(src, dst)
	if err != nil {
		t.Fatalf("copyFile error: %v", err)
	}

	// Verify content
	dstContent, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading dst: %v", err)
	}
	if string(dstContent) != string(content) {
		t.Errorf("copied content = %q, want %q", string(dstContent), string(content))
	}

	// Verify permissions
	srcInfo, _ := os.Stat(src)
	dstInfo, _ := os.Stat(dst)
	if srcInfo.Mode() != dstInfo.Mode() {
		t.Errorf("copied mode = %v, want %v", dstInfo.Mode(), srcInfo.Mode())
	}
}

// TestSyncManager_DetectChanges verifies that SyncManager correctly identifies new, updated, and unchanged tools.
// Creates staging and approved directories with different file states.
//
// TestSyncManager_DetectChangesはSyncManagerが新規、更新、未変更のツールを正しく識別することを確認します。
// ステージングと承認済みディレクトリに異なる状態のファイルを作成してテストします。
func TestSyncManager_DetectChanges(t *testing.T) {
	workspaceDir := t.TempDir()
	approvedBaseDir := t.TempDir()

	// Create staging directory with tools
	stagingDir := filepath.Join(workspaceDir, "host-tools")
	os.MkdirAll(stagingDir, 0755)

	os.WriteFile(filepath.Join(stagingDir, "new-tool.sh"),
		[]byte("#!/bin/bash\n# new-tool.sh\n# A new tool\necho new\n"), 0755)
	os.WriteFile(filepath.Join(stagingDir, "existing-tool.sh"),
		[]byte("#!/bin/bash\n# existing-tool.sh\n# Updated content\necho updated\n"), 0755)
	os.WriteFile(filepath.Join(stagingDir, "unchanged-tool.sh"),
		[]byte("#!/bin/bash\n# unchanged-tool.sh\n# Same content\necho same\n"), 0755)

	// Create approved directory with some existing tools
	projectID := ProjectID(workspaceDir)
	approvedDir := filepath.Join(approvedBaseDir, projectID)
	os.MkdirAll(approvedDir, 0755)

	os.WriteFile(filepath.Join(approvedDir, "existing-tool.sh"),
		[]byte("#!/bin/bash\n# existing-tool.sh\n# Old content\necho old\n"), 0755)
	os.WriteFile(filepath.Join(approvedDir, "unchanged-tool.sh"),
		[]byte("#!/bin/bash\n# unchanged-tool.sh\n# Same content\necho same\n"), 0755)

	cfg := &config.HostToolsConfig{
		Enabled:           true,
		ApprovedDir:       approvedBaseDir,
		StagingDirs:       []string{"host-tools"},
		AllowedExtensions: []string{".sh"},
		Timeout:           30,
	}

	syncMgr := NewSyncManager(cfg, workspaceDir)
	items, err := syncMgr.DetectChanges()
	if err != nil {
		t.Fatalf("DetectChanges error: %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("DetectChanges returned %d items, want 3", len(items))
	}

	// Build a map for easier assertions
	statusMap := make(map[string]SyncStatus)
	for _, item := range items {
		statusMap[item.Name] = item.Status
	}

	if statusMap["new-tool.sh"] != SyncNew {
		t.Errorf("new-tool.sh status = %d, want SyncNew (%d)", statusMap["new-tool.sh"], SyncNew)
	}
	if statusMap["existing-tool.sh"] != SyncUpdated {
		t.Errorf("existing-tool.sh status = %d, want SyncUpdated (%d)", statusMap["existing-tool.sh"], SyncUpdated)
	}
	if statusMap["unchanged-tool.sh"] != SyncUnchanged {
		t.Errorf("unchanged-tool.sh status = %d, want SyncUnchanged (%d)", statusMap["unchanged-tool.sh"], SyncUnchanged)
	}
}

// TestSyncManager_RunInteractiveSync_ApproveAll verifies that tools are synced when user approves.
// Simulates user input "y" and verifies the tool is copied to approved directory.
//
// TestSyncManager_RunInteractiveSync_ApproveAllはユーザーが承認した場合にツールが同期されることを確認します。
// ユーザー入力"y"をシミュレートし、ツールが承認済みディレクトリにコピーされることを検証します。
func TestSyncManager_RunInteractiveSync_ApproveAll(t *testing.T) {
	workspaceDir := t.TempDir()
	approvedBaseDir := t.TempDir()

	// Create staging directory with a new tool
	stagingDir := filepath.Join(workspaceDir, "host-tools")
	os.MkdirAll(stagingDir, 0755)
	os.WriteFile(filepath.Join(stagingDir, "tool.sh"),
		[]byte("#!/bin/bash\n# tool.sh\n# Test tool\necho test\n"), 0755)

	cfg := &config.HostToolsConfig{
		Enabled:           true,
		ApprovedDir:       approvedBaseDir,
		StagingDirs:       []string{"host-tools"},
		AllowedExtensions: []string{".sh"},
		Timeout:           30,
	}

	syncMgr := NewSyncManager(cfg, workspaceDir)
	syncMgr.SetReader(strings.NewReader("y\n"))
	syncMgr.SetWriter(io.Discard)

	synced, err := syncMgr.RunInteractiveSync()
	if err != nil {
		t.Fatalf("RunInteractiveSync error: %v", err)
	}
	if synced != 1 {
		t.Errorf("synced = %d, want 1", synced)
	}

	// Verify tool was copied
	projectID := ProjectID(workspaceDir)
	approvedPath := filepath.Join(approvedBaseDir, projectID, "tool.sh")
	if _, err := os.Stat(approvedPath); os.IsNotExist(err) {
		t.Errorf("tool.sh not copied to approved directory: %s", approvedPath)
	}
}

// TestSyncManager_RunInteractiveSync_DenyAll verifies that tools are not synced when user denies.
// Simulates user input "n" and verifies the tool is not copied.
//
// TestSyncManager_RunInteractiveSync_DenyAllはユーザーが拒否した場合にツールが同期されないことを確認します。
// ユーザー入力"n"をシミュレートし、ツールがコピーされないことを検証します。
func TestSyncManager_RunInteractiveSync_DenyAll(t *testing.T) {
	workspaceDir := t.TempDir()
	approvedBaseDir := t.TempDir()

	stagingDir := filepath.Join(workspaceDir, "host-tools")
	os.MkdirAll(stagingDir, 0755)
	os.WriteFile(filepath.Join(stagingDir, "tool.sh"),
		[]byte("#!/bin/bash\n# tool.sh\n# Test tool\necho test\n"), 0755)

	cfg := &config.HostToolsConfig{
		Enabled:           true,
		ApprovedDir:       approvedBaseDir,
		StagingDirs:       []string{"host-tools"},
		AllowedExtensions: []string{".sh"},
		Timeout:           30,
	}

	syncMgr := NewSyncManager(cfg, workspaceDir)
	syncMgr.SetReader(strings.NewReader("n\n"))
	syncMgr.SetWriter(io.Discard)

	synced, err := syncMgr.RunInteractiveSync()
	if err != nil {
		t.Fatalf("RunInteractiveSync error: %v", err)
	}
	if synced != 0 {
		t.Errorf("synced = %d, want 0", synced)
	}

	// Verify tool was NOT copied
	projectID := ProjectID(workspaceDir)
	approvedPath := filepath.Join(approvedBaseDir, projectID, "tool.sh")
	if _, err := os.Stat(approvedPath); !os.IsNotExist(err) {
		t.Errorf("tool.sh should NOT have been copied to %s", approvedPath)
	}
}

// TestSyncManager_RunInteractiveSync_NoChanges verifies that no sync occurs when all tools are unchanged.
// Creates identical tools in both staging and approved directories.
//
// TestSyncManager_RunInteractiveSync_NoChangesはすべてのツールが未変更の場合に同期が発生しないことを確認します。
// ステージングと承認済みディレクトリに同一のツールを作成してテストします。
func TestSyncManager_RunInteractiveSync_NoChanges(t *testing.T) {
	workspaceDir := t.TempDir()
	approvedBaseDir := t.TempDir()

	// Create same tool in both staging and approved
	stagingDir := filepath.Join(workspaceDir, "host-tools")
	os.MkdirAll(stagingDir, 0755)
	content := []byte("#!/bin/bash\n# tool.sh\n# Same tool\necho same\n")
	os.WriteFile(filepath.Join(stagingDir, "tool.sh"), content, 0755)

	projectID := ProjectID(workspaceDir)
	approvedDir := filepath.Join(approvedBaseDir, projectID)
	os.MkdirAll(approvedDir, 0755)
	os.WriteFile(filepath.Join(approvedDir, "tool.sh"), content, 0755)

	cfg := &config.HostToolsConfig{
		Enabled:           true,
		ApprovedDir:       approvedBaseDir,
		StagingDirs:       []string{"host-tools"},
		AllowedExtensions: []string{".sh"},
		Timeout:           30,
	}

	syncMgr := NewSyncManager(cfg, workspaceDir)
	syncMgr.SetWriter(io.Discard)

	synced, err := syncMgr.RunInteractiveSync()
	if err != nil {
		t.Fatalf("RunInteractiveSync error: %v", err)
	}
	if synced != 0 {
		t.Errorf("synced = %d, want 0 (no changes)", synced)
	}
}

// TestSyncManager_RunInteractiveSync_NewTool_ShowsTimeoutDeclaration verifies
// that a new tool's "@timeout:" declaration is shown in the approval prompt
// unconditionally (before the [y/N] question is even asked), not hidden
// behind an opt-in diff command. This is what makes the human review this
// feature depends on actually happen for brand-new tools.
//
// TestSyncManager_RunInteractiveSync_NewTool_ShowsTimeoutDeclarationは、
// 新規ツールの「@timeout:」宣言が、オプトインのdiffコマンドの裏に隠されず、
// [y/N]と聞かれる前に承認プロンプトへ無条件で表示されることを確認します。
// これにより、本機能が前提とする人間のレビューが新規ツールでも実際に
// 行われるようになります。
func TestSyncManager_RunInteractiveSync_NewTool_ShowsTimeoutDeclaration(t *testing.T) {
	workspaceDir := t.TempDir()
	approvedBaseDir := t.TempDir()

	stagingDir := filepath.Join(workspaceDir, "host-tools")
	os.MkdirAll(stagingDir, 0755)
	os.WriteFile(filepath.Join(stagingDir, "tool.sh"),
		[]byte("#!/bin/bash\n# tool.sh\n# @timeout: 900\n# Test tool\necho test\n"), 0755)

	cfg := &config.HostToolsConfig{
		Enabled:           true,
		ApprovedDir:       approvedBaseDir,
		StagingDirs:       []string{"host-tools"},
		AllowedExtensions: []string{".sh"},
		Timeout:           60,
		MaxToolTimeout:    1800,
	}

	syncMgr := NewSyncManager(cfg, workspaceDir)
	syncMgr.SetReader(strings.NewReader("n\n"))
	var out bytes.Buffer
	syncMgr.SetWriter(&out)

	if _, err := syncMgr.RunInteractiveSync(); err != nil {
		t.Fatalf("RunInteractiveSync error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "@timeout: 900") {
		t.Errorf("expected the @timeout declaration to appear in the prompt output, got:\n%s", output)
	}
	// The warning must appear before the [y/N] question, i.e. unconditionally,
	// not only reachable via the "d(iff)" command.
	// 警告は[y/N]の質問より前に、つまり「d(iff)」コマンド経由でしか
	// 到達できない形ではなく、無条件に表示されなければならない。
	warnIdx := strings.Index(output, "@timeout: 900")
	promptIdx := strings.Index(output, "[y/N]")
	if warnIdx == -1 || promptIdx == -1 || warnIdx > promptIdx {
		t.Errorf("expected the timeout warning to appear before the [y/N] prompt, got:\n%s", output)
	}
}

// TestSyncManager_RunInteractiveSync_UpdatedTool_ShowsTimeoutDeclaration is
// the same check as above for the SyncUpdated path, which previously only
// showed file content via an opt-in "d(iff)" command.
//
// TestSyncManager_RunInteractiveSync_UpdatedTool_ShowsTimeoutDeclarationは、
// 上記と同じ確認をSyncUpdated経路に対して行います。以前はオプトインの
// 「d(iff)」コマンド経由でしかファイル内容を表示していませんでした。
func TestSyncManager_RunInteractiveSync_UpdatedTool_ShowsTimeoutDeclaration(t *testing.T) {
	workspaceDir := t.TempDir()
	approvedBaseDir := t.TempDir()

	stagingDir := filepath.Join(workspaceDir, "host-tools")
	os.MkdirAll(stagingDir, 0755)
	os.WriteFile(filepath.Join(stagingDir, "tool.sh"),
		[]byte("#!/bin/bash\n# tool.sh\n# @timeout: 900\n# Updated tool\necho updated\n"), 0755)

	projectID := ProjectID(workspaceDir)
	approvedDir := filepath.Join(approvedBaseDir, projectID)
	os.MkdirAll(approvedDir, 0755)
	os.WriteFile(filepath.Join(approvedDir, "tool.sh"),
		[]byte("#!/bin/bash\n# tool.sh\n# Old tool\necho old\n"), 0755)

	cfg := &config.HostToolsConfig{
		Enabled:           true,
		ApprovedDir:       approvedBaseDir,
		StagingDirs:       []string{"host-tools"},
		AllowedExtensions: []string{".sh"},
		Timeout:           60,
		MaxToolTimeout:    1800,
	}

	syncMgr := NewSyncManager(cfg, workspaceDir)
	syncMgr.SetReader(strings.NewReader("n\n"))
	var out bytes.Buffer
	syncMgr.SetWriter(&out)

	if _, err := syncMgr.RunInteractiveSync(); err != nil {
		t.Fatalf("RunInteractiveSync error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "@timeout: 900") {
		t.Errorf("expected the @timeout declaration to appear in the prompt output, got:\n%s", output)
	}
}

// TestSyncManager_RunInteractiveSync_NoTimeoutDeclaration_NoWarningShown is a
// regression test: a tool that does not declare "@timeout:" must not trigger
// the warning line, so the approval prompt for ordinary tools is unchanged
// from before this feature was added.
//
// TestSyncManager_RunInteractiveSync_NoTimeoutDeclaration_NoWarningShownは
// 回帰テストです。「@timeout:」を宣言していないツールでは警告行が表示されず、
// 通常ツールの承認プロンプトが本機能追加前と変わらないことを確認します。
func TestSyncManager_RunInteractiveSync_NoTimeoutDeclaration_NoWarningShown(t *testing.T) {
	workspaceDir := t.TempDir()
	approvedBaseDir := t.TempDir()

	stagingDir := filepath.Join(workspaceDir, "host-tools")
	os.MkdirAll(stagingDir, 0755)
	os.WriteFile(filepath.Join(stagingDir, "tool.sh"),
		[]byte("#!/bin/bash\n# tool.sh\n# Plain tool\necho test\n"), 0755)

	cfg := &config.HostToolsConfig{
		Enabled:           true,
		ApprovedDir:       approvedBaseDir,
		StagingDirs:       []string{"host-tools"},
		AllowedExtensions: []string{".sh"},
		Timeout:           60,
		MaxToolTimeout:    1800,
	}

	syncMgr := NewSyncManager(cfg, workspaceDir)
	syncMgr.SetReader(strings.NewReader("n\n"))
	var out bytes.Buffer
	syncMgr.SetWriter(&out)

	if _, err := syncMgr.RunInteractiveSync(); err != nil {
		t.Fatalf("RunInteractiveSync error: %v", err)
	}

	if strings.Contains(out.String(), "@timeout") {
		t.Errorf("expected no timeout warning for a tool without a declaration, got:\n%s", out.String())
	}
}

// TestSyncManager_PrintTimeoutWarning_Locale verifies that the timeout
// warning line respects the LC_ALL/LANG-based locale switch used elsewhere
// in this codebase (see isJapaneseLocale in internal/cli/client.go and
// writeSponsorMessage in internal/cli/serve.go): Japanese when LC_ALL or
// LANG starts with "ja_JP" (LC_ALL taking precedence per POSIX), English
// otherwise — and confirms the "@timeout: N" token itself is always present
// in either language, since other tests key off that literal substring.
//
// TestSyncManager_PrintTimeoutWarning_Localeは、タイムアウト警告行が、
// このコードベースの他の箇所（internal/cli/client.goのisJapaneseLocaleや
// internal/cli/serve.goのwriteSponsorMessage）と同じLC_ALL/LANGベースの
// 言語切り替えに従うことを確認します: LC_ALLまたはLANGが"ja_JP"で始まれば
// 日本語（POSIXの優先順位に従いLC_ALLが優先）、そうでなければ英語になります。
// また、どちらの言語でも「@timeout: N」というトークン自体は必ず含まれる
// ことも確認します。他のテストがこのリテラル部分文字列に依存しているためです。
func TestSyncManager_PrintTimeoutWarning_Locale(t *testing.T) {
	tests := []struct {
		name     string
		lang     string
		lcAll    string
		wantText string
	}{
		{name: "English by default", lang: "en_US.UTF-8", lcAll: "", wantText: "Declares custom timeout"},
		{name: "Japanese via LANG", lang: "ja_JP.UTF-8", lcAll: "", wantText: "独自のタイムアウトを宣言"},
		{name: "Japanese via LC_ALL", lang: "en_US.UTF-8", lcAll: "ja_JP.UTF-8", wantText: "独自のタイムアウトを宣言"},
		{name: "LC_ALL overrides LANG to English", lang: "ja_JP.UTF-8", lcAll: "en_US.UTF-8", wantText: "Declares custom timeout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LANG", tt.lang)
			t.Setenv("LC_ALL", tt.lcAll)

			cfg := &config.HostToolsConfig{Timeout: 60, MaxToolTimeout: 1800}
			syncMgr := NewSyncManager(cfg, t.TempDir())
			var out bytes.Buffer
			syncMgr.SetWriter(&out)

			syncMgr.printTimeoutWarning(SyncItem{Name: "tool.sh", Timeout: 900})

			output := out.String()
			if !strings.Contains(output, tt.wantText) {
				t.Errorf("expected output to contain %q, got: %q", tt.wantText, output)
			}
			if !strings.Contains(output, "@timeout: 900") {
				t.Errorf("expected the literal '@timeout: 900' token regardless of locale, got: %q", output)
			}
		})
	}
}

// TestWriteProjectMeta verifies that project metadata file is created with workspace path information.
//
// TestWriteProjectMetaはプロジェクトメタデータファイルがワークスペースパス情報とともに作成されることを確認します。
func TestWriteProjectMeta(t *testing.T) {
	dir := t.TempDir()

	err := writeProjectMeta(dir, "/home/user/my-project")
	if err != nil {
		t.Fatalf("writeProjectMeta error: %v", err)
	}

	metaPath := filepath.Join(dir, projectMetaFile)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("reading metadata: %v", err)
	}

	if !strings.Contains(string(data), "/home/user/my-project") {
		t.Errorf("metadata does not contain workspace path: %s", string(data))
	}
}
