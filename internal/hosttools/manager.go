package hosttools

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/YujiSuzuki/hostmcp/internal/config"
)

// Manager coordinates tool discovery and execution for host tools.
// Supports two modes:
//   - Legacy mode: tools loaded from Directories (when ApprovedDir is empty)
//   - Secure mode: tools loaded from approved directory + optional common directory
//
// In secure mode, --dev flag enables development mode where staging directories
// are also included with highest priority (staging > approved > common).
//
// Managerはホストツールの検出と実行を調整します。
// 2つのモードをサポートします:
//   - レガシーモード: Directoriesからツールを読み込み（ApprovedDirが空の場合）
//   - セキュアモード: 承認済みディレクトリ + オプションの共通ディレクトリからツールを読み込み
//
// セキュアモードでは、--devフラグで開発モードを有効にでき、
// ステージングディレクトリも最優先で読み込まれます（staging > approved > common）。
type Manager struct {
	config        *config.HostToolsConfig
	workspaceRoot string
	devMode       bool
}

// NewManager creates a new host tools manager.
// NewManagerは新しいホストツールマネージャーを作成します。
func NewManager(cfg *config.HostToolsConfig, workspaceRoot string) *Manager {
	return &Manager{
		config:        cfg,
		workspaceRoot: workspaceRoot,
	}
}

// IsEnabled returns whether host tools are enabled.
// IsEnabledはホストツールが有効かどうかを返します。
func (m *Manager) IsEnabled() bool {
	return m.config != nil && m.config.Enabled
}

// IsSecureMode returns whether secure mode is active.
// IsSecureModeはセキュアモードが有効かどうかを返します。
func (m *Manager) IsSecureMode() bool {
	return m.config != nil && m.config.IsSecureMode()
}

// Config returns the host tools configuration.
// Configはホストツールの設定を返します。
func (m *Manager) Config() *config.HostToolsConfig {
	return m.config
}

// SetDevMode enables development mode.
// In dev mode, staging directories are included with highest priority,
// allowing tools under development to be tested without approval.
//
// SetDevModeは開発モードを有効にします。
// 開発モードでは、ステージングディレクトリが最優先で読み込まれ、
// 承認なしで開発中のツールをテストできます。
func (m *Manager) SetDevMode(enabled bool) {
	m.devMode = enabled
}

// IsDevMode returns whether development mode is active.
// IsDevModeは開発モードが有効かどうかを返します。
func (m *Manager) IsDevMode() bool {
	return m.devMode
}

// toolDirs returns the directories to load tools from based on the current mode.
// toolDirsは現在のモードに基づいてツールを読み込むディレクトリを返します。
func (m *Manager) toolDirs() ([]string, error) {
	if !m.config.IsSecureMode() {
		// Legacy mode: use configured directories
		// レガシーモード: 設定されたディレクトリを使用
		var dirs []string
		for _, dir := range m.config.Directories {
			dirs = append(dirs, m.resolveDir(dir))
		}
		return dirs, nil
	}

	// Secure mode: use approved directory + optional common
	// Dev mode adds staging dirs with highest priority
	//
	// セキュアモード: 承認済みディレクトリ + オプションの共通ディレクトリ
	// 開発モードではステージングディレクトリを最優先で追加
	var dirs []string

	// Dev mode: staging directories first (highest priority)
	// 開発モード: ステージングディレクトリを最優先
	if m.devMode {
		stagingDirs := m.config.StagingDirs
		if len(stagingDirs) == 0 {
			stagingDirs = m.config.Directories
		}
		for _, dir := range stagingDirs {
			dirs = append(dirs, m.resolveDir(dir))
		}
	}

	projectDir, err := ProjectApprovedDir(m.config.ApprovedDir, m.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving approved directory: %w", err)
	}
	dirs = append(dirs, projectDir)

	if m.config.Common {
		commonDir, err := CommonApprovedDir(m.config.ApprovedDir)
		if err != nil {
			slog.Debug("Cannot resolve common directory", "error", err)
		} else {
			dirs = append(dirs, commonDir)
		}
	}

	return dirs, nil
}

// ListTools returns metadata for all discovered tools across all configured directories.
// ListToolsはすべての設定されたディレクトリ内の発見されたツールのメタデータを返します。
func (m *Manager) ListTools() ([]ToolInfo, error) {
	if !m.IsEnabled() {
		return nil, fmt.Errorf("host tools are disabled")
	}

	dirs, err := m.toolDirs()
	if err != nil {
		return nil, err
	}

	var allTools []ToolInfo
	seen := make(map[string]bool) // Deduplicate by name (project takes priority over common)
	for _, dir := range dirs {
		tools, err := ListTools(dir, m.config.AllowedExtensions)
		if err != nil {
			// Skip directories that don't exist or can't be read
			// 存在しないまたは読み取れないディレクトリはスキップ
			continue
		}
		for _, tool := range tools {
			if !seen[tool.Name] {
				seen[tool.Name] = true
				allTools = append(allTools, tool)
			}
		}
	}
	return allTools, nil
}

// GetToolInfo returns detailed info for a specific tool by name.
// It searches all configured directories.
//
// GetToolInfoは名前で指定されたツールの詳細情報を返します。
// すべての設定されたディレクトリを検索します。
func (m *Manager) GetToolInfo(name string) (ToolInfo, error) {
	if !m.IsEnabled() {
		return ToolInfo{}, fmt.Errorf("host tools are disabled")
	}

	dirs, err := m.toolDirs()
	if err != nil {
		return ToolInfo{}, err
	}

	for _, dir := range dirs {
		info, err := GetToolInfo(dir, name, m.config.AllowedExtensions)
		if err == nil {
			return info, nil
		}
	}
	return ToolInfo{}, fmt.Errorf("tool not found: %s", name)
}

// RunTool executes a tool by name with the given arguments.
// It searches all configured directories for the tool.
//
// RunToolは名前で指定されたツールを引数付きで実行します。
// すべての設定されたディレクトリでツールを検索します。
func (m *Manager) RunTool(name string, args []string) (*Result, error) {
	if !m.IsEnabled() {
		return nil, fmt.Errorf("host tools are disabled")
	}

	dirs, err := m.toolDirs()
	if err != nil {
		return nil, err
	}

	for _, dir := range dirs {
		// Check if tool exists in this directory and pick up its own
		// declared timeout (if any) via GetToolInfo.
		// このディレクトリにツールが存在するか確認し、GetToolInfoでツール自身が
		// 宣言するタイムアウト（あれば）も取得する。
		info, err := GetToolInfo(dir, name, m.config.AllowedExtensions)
		if err != nil {
			continue
		}
		return RunTool(dir, name, args, m.effectiveTimeout(info), m.workspaceRoot)
	}
	return nil, fmt.Errorf("tool not found: %s", name)
}

// resolveTimeout computes the actual timeout to use for a tool run: its own
// declared "@timeout:" value if present, clamped to MaxToolTimeout (unless
// MaxToolTimeout is 0, meaning no ceiling); otherwise the global default. It
// also reports whether clamping occurred, so callers can decide for
// themselves whether that fact is worth logging (see effectiveTimeout vs.
// EffectiveTimeout below — only the former logs, since only it corresponds
// to a run that is actually about to happen on the host).
//
// resolveTimeoutは、ツール実行に実際に使うタイムアウトを計算します:
// ツール自身が宣言した「@timeout:」値があればそれを使い、MaxToolTimeoutで
// クランプします（MaxToolTimeoutが0の場合は上限なし）。宣言が無ければ
// グローバル既定値を使います。クランプが発生したかどうかも返すため、
// 呼び出し元はそれをログに残す価値があるかどうかを自分で判断できます
// （下記のeffectiveTimeoutとEffectiveTimeoutの違いを参照 — 実際にホスト上で
// 実行が始まろうとしているeffectiveTimeoutのみがログを出します）。
func (m *Manager) resolveTimeout(info ToolInfo) (effective time.Duration, declared int, clampedTo int, wasClamped bool) {
	declared = m.config.Timeout
	if info.Timeout > 0 {
		declared = info.Timeout
	}
	eff := declared
	if m.config.MaxToolTimeout > 0 && eff > m.config.MaxToolTimeout {
		wasClamped = true
		clampedTo = m.config.MaxToolTimeout
		eff = m.config.MaxToolTimeout
	}
	return time.Duration(eff) * time.Second, declared, clampedTo, wasClamped
}

// effectiveTimeout resolves the timeout for a tool run that is actually about
// to happen, logging a warning if the declared value gets clamped.
//
// effectiveTimeoutは、これから実際に行われるツール実行のタイムアウトを
// 解決します。宣言値がクランプされた場合は警告をログに出します。
func (m *Manager) effectiveTimeout(info ToolInfo) time.Duration {
	eff, declared, clampedTo, wasClamped := m.resolveTimeout(info)
	if wasClamped {
		slog.Warn("Clamping declared tool timeout to max_tool_timeout ceiling",
			"tool", info.Name, "declared", declared, "clamped_to", clampedTo)
	}
	return eff
}

// EffectiveTimeout resolves the timeout RunTool would actually apply to the
// named tool, without running it and without logging — callers use this to
// pre-check whether their own wait budget is long enough before starting a
// potentially long-running host tool, and the tool may end up never running
// at all (e.g. the pre-check itself fails), so it must not emit the same
// warning effectiveTimeout does for a real run.
//
// EffectiveTimeoutは、指定されたツールをRunToolが実行した場合に実際に
// 適用されるタイムアウト値を、ツールを実行せず、ログも出さずに解決します。
// 呼び出し元はこれを使って、時間のかかる可能性のあるホストツールを開始する
// 前に、自身の待ち時間予算が十分かどうかを事前にチェックできます。この
// 事前チェック自体が失敗してツールが結局一度も実行されない場合もあるため、
// effectiveTimeoutが実際の実行時に出すのと同じ警告をここで出してはいけません。
func (m *Manager) EffectiveTimeout(name string) (time.Duration, error) {
	info, err := m.GetToolInfo(name)
	if err != nil {
		return 0, err
	}
	eff, _, _, _ := m.resolveTimeout(info)
	return eff, nil
}

// PendingApproval returns the host tools that are staged (in the staging
// directories) but not yet approved, or whose staged content differs from
// the currently approved copy. It only applies in secure mode — legacy mode
// has no staging/approval workflow, so it returns nil.
//
// PendingApprovalはステージングディレクトリにあるがまだ承認されていない、
// またはステージング内容が承認済みコピーと異なるホストツールを返します。
// セキュアモードでのみ適用されます — レガシーモードには
// ステージング/承認ワークフローがないため、nilを返します。
func (m *Manager) PendingApproval() ([]SyncItem, error) {
	if !m.IsEnabled() {
		return nil, fmt.Errorf("host tools are disabled")
	}
	if !m.IsSecureMode() {
		return nil, nil
	}

	syncMgr := NewSyncManager(m.config, m.workspaceRoot)
	items, err := syncMgr.DetectChanges()
	if err != nil {
		return nil, err
	}

	var pending []SyncItem
	for _, item := range items {
		if item.Status != SyncUnchanged {
			pending = append(pending, item)
		}
	}
	return pending, nil
}

// resolveDir resolves a directory path relative to workspaceRoot.
// resolveDirはworkspaceRootからの相対ディレクトリパスを解決します。
func (m *Manager) resolveDir(dir string) string {
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(m.workspaceRoot, dir)
}
