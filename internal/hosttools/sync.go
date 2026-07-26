// sync.go implements the tool approval workflow for secure mode.
// It compares tools in staging directories (workspace) with approved tools,
// prompts the user for approval, and copies approved tools to the approved directory.
//
// sync.goはセキュアモードのツール承認ワークフローを実装します。
// ステージングディレクトリ（ワークスペース）のツールと承認済みツールを比較し、
// ユーザーに承認を求め、承認されたツールを承認済みディレクトリにコピーします。
package hosttools

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/YujiSuzuki/hostmcp/internal/config"
)

// SyncStatus represents the status of a tool comparison between staging and approved.
// SyncStatusはステージングと承認済みのツール比較のステータスを表します。
type SyncStatus int

const (
	// SyncNew indicates a tool exists in staging but not in approved.
	// SyncNewはステージングにあるが承認済みにないツールを示します。
	SyncNew SyncStatus = iota

	// SyncUpdated indicates a tool exists in both but content differs.
	// SyncUpdatedは両方にあるがコンテンツが異なるツールを示します。
	SyncUpdated

	// SyncUnchanged indicates a tool exists in both with identical content.
	// SyncUnchangedは両方にあり、同一コンテンツのツールを示します。
	SyncUnchanged
)

// SyncItem represents a tool that may need syncing.
// SyncItemは同期が必要な可能性のあるツールを表します。
type SyncItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Timeout is the tool's own declared "@timeout: N" value (seconds), or 0
	// if it did not declare one. Carried through from ToolInfo so the
	// interactive approval prompt can surface it — this is what lets a human
	// reviewer actually see a per-tool timeout declaration before approving,
	// rather than relying solely on catching it in the diff view.
	//
	// Timeoutは、ツール自身が宣言した「@timeout: N」の値（秒）で、未宣言の
	// 場合は0です。ToolInfoから引き継ぎ、対話承認プロンプトに表示できるように
	// します — これにより、人間のレビュアーはdiff表示に気づくかどうかに頼らず、
	// ツール別タイムアウト宣言を承認前に実際に目にすることができます。
	Timeout      int        `json:"timeout,omitempty"`
	Status       SyncStatus `json:"status"`
	StagingPath  string     `json:"staging_path"`
	ApprovedPath string     `json:"approved_path"`
}

// projectMeta stores metadata about a project in the approved directory.
// projectMetaは承認済みディレクトリ内のプロジェクトメタデータを格納します。
type projectMeta struct {
	Workspace string `json:"workspace"`
}

const (
	// projectMetaFile is the name of the metadata file in each project directory.
	projectMetaFile = ".project"

	// commonDirName is the name of the shared tools subdirectory.
	commonDirName = "_common"
)

// ProjectID generates a human-readable project identifier from a workspace path.
// Format: <dir-name>-<short-hash> (e.g., "my-project-a1b2c3d4")
//
// ProjectIDはワークスペースパスから人間が読めるプロジェクト識別子を生成します。
// 形式: <dir-name>-<short-hash>（例: "my-project-a1b2c3d4"）
func ProjectID(workspacePath string) string {
	absPath, err := filepath.Abs(workspacePath)
	if err != nil {
		absPath = workspacePath
	}

	dirName := filepath.Base(absPath)
	// Sanitize directory name: keep only alphanumeric, dash, underscore
	// ディレクトリ名をサニタイズ: 英数字、ダッシュ、アンダースコアのみ保持
	var sanitized strings.Builder
	for _, ch := range dirName {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			sanitized.WriteRune(ch)
		}
	}
	name := sanitized.String()
	if name == "" {
		name = "project"
	}

	hash := sha256.Sum256([]byte(absPath))
	shortHash := fmt.Sprintf("%x", hash[:4])

	return name + "-" + shortHash
}

// ResolveApprovedDir returns the absolute path to the approved directory,
// expanding ~ to the user's home directory.
//
// ResolveApprovedDirは承認済みディレクトリの絶対パスを返します。
// ~をユーザーのホームディレクトリに展開します。
func ResolveApprovedDir(approvedDir string) (string, error) {
	if strings.HasPrefix(approvedDir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home directory: %w", err)
		}
		approvedDir = filepath.Join(home, approvedDir[2:])
	}
	return filepath.Abs(approvedDir)
}

// ProjectApprovedDir returns the per-project approved directory path.
// ProjectApprovedDirはプロジェクトごとの承認済みディレクトリパスを返します。
func ProjectApprovedDir(approvedDir, workspacePath string) (string, error) {
	resolved, err := ResolveApprovedDir(approvedDir)
	if err != nil {
		return "", err
	}
	projectID := ProjectID(workspacePath)
	return filepath.Join(resolved, projectID), nil
}

// CommonApprovedDir returns the common tools directory path.
// CommonApprovedDirは共通ツールディレクトリパスを返します。
func CommonApprovedDir(approvedDir string) (string, error) {
	resolved, err := ResolveApprovedDir(approvedDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, commonDirName), nil
}

// SyncManager handles the tool approval workflow.
// SyncManagerはツール承認ワークフローを処理します。
type SyncManager struct {
	cfg           *config.HostToolsConfig
	workspaceRoot string
	reader        io.Reader // for testing: override stdin
	writer        io.Writer // for testing: override stdout
}

// NewSyncManager creates a new SyncManager.
// NewSyncManagerは新しいSyncManagerを作成します。
func NewSyncManager(cfg *config.HostToolsConfig, workspaceRoot string) *SyncManager {
	return &SyncManager{
		cfg:           cfg,
		workspaceRoot: workspaceRoot,
		reader:        os.Stdin,
		writer:        os.Stdout,
	}
}

// SetReader overrides the input reader (for testing).
// SetReaderは入力リーダーを上書きします（テスト用）。
func (s *SyncManager) SetReader(r io.Reader) {
	s.reader = r
}

// SetWriter overrides the output writer (for testing).
// SetWriterは出力ライターを上書きします（テスト用）。
func (s *SyncManager) SetWriter(w io.Writer) {
	s.writer = w
}

// DetectChanges compares staging directories with the approved directory
// and returns a list of tools that need attention.
//
// DetectChangesはステージングディレクトリと承認済みディレクトリを比較し、
// 注意が必要なツールのリストを返します。
func (s *SyncManager) DetectChanges() ([]SyncItem, error) {
	approvedDir, err := ProjectApprovedDir(s.cfg.ApprovedDir, s.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving approved directory: %w", err)
	}

	var items []SyncItem

	stagingDirs := s.cfg.StagingDirs
	if len(stagingDirs) == 0 {
		stagingDirs = s.cfg.Directories
	}

	for _, dir := range stagingDirs {
		absDir := dir
		if !filepath.IsAbs(dir) {
			absDir = filepath.Join(s.workspaceRoot, dir)
		}

		stagingTools, err := ListTools(absDir, s.cfg.AllowedExtensions)
		if err != nil {
			slog.Debug("Skipping staging directory", "dir", absDir, "error", err)
			continue
		}

		for _, tool := range stagingTools {
			stagingPath := filepath.Join(absDir, tool.Name)
			approvedPath := filepath.Join(approvedDir, tool.Name)

			status, err := compareFiles(stagingPath, approvedPath)
			if err != nil {
				return nil, fmt.Errorf("comparing %s: %w", tool.Name, err)
			}

			items = append(items, SyncItem{
				Name:         tool.Name,
				Description:  tool.Description,
				Timeout:      tool.Timeout,
				Status:       status,
				StagingPath:  stagingPath,
				ApprovedPath: approvedPath,
			})
		}
	}

	return items, nil
}

// RunInteractiveSync performs an interactive sync session.
// For each new or updated tool, it prompts the user for approval.
// Returns the number of tools synced.
//
// RunInteractiveSyncはインタラクティブな同期セッションを実行します。
// 新しいまたは更新されたツールごとに、ユーザーに承認を求めます。
// 同期されたツールの数を返します。
func (s *SyncManager) RunInteractiveSync() (int, error) {
	items, err := s.DetectChanges()
	if err != nil {
		return 0, err
	}

	approvedDir, err := ProjectApprovedDir(s.cfg.ApprovedDir, s.workspaceRoot)
	if err != nil {
		return 0, err
	}

	// Ensure approved directory exists
	// 承認済みディレクトリが存在することを確認
	if err := os.MkdirAll(approvedDir, 0755); err != nil {
		return 0, fmt.Errorf("creating approved directory %s: %w", approvedDir, err)
	}

	// Write project metadata
	// プロジェクトメタデータを書き込み
	if err := writeProjectMeta(approvedDir, s.workspaceRoot); err != nil {
		slog.Warn("Failed to write project metadata", "error", err)
	}

	hasChanges := false
	for _, item := range items {
		if item.Status != SyncUnchanged {
			hasChanges = true
			break
		}
	}

	if !hasChanges {
		fmt.Fprintln(s.writer, "All tools are up to date. No sync needed.")
		return 0, nil
	}

	fmt.Fprintln(s.writer)
	fmt.Fprintln(s.writer, "🔍 Checking host tools...")
	fmt.Fprintln(s.writer)

	scanner := bufio.NewScanner(s.reader)
	synced := 0

	for _, item := range items {
		switch item.Status {
		case SyncUnchanged:
			fmt.Fprintf(s.writer, "  Unchanged: %s (skipped)\n", item.Name)

		case SyncNew:
			fmt.Fprintf(s.writer, "  New tool found:\n")
			fmt.Fprintf(s.writer, "    %s", item.Name)
			if item.Description != "" {
				fmt.Fprintf(s.writer, " - %q", item.Description)
			}
			fmt.Fprintln(s.writer)
			fmt.Fprintf(s.writer, "    Source: %s\n", item.StagingPath)
			s.printTimeoutWarning(item)
			fmt.Fprintf(s.writer, "    → Copy to %s? [y/N] ", item.ApprovedPath)

			if scanner.Scan() {
				answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if answer == "y" || answer == "yes" {
					if err := copyFile(item.StagingPath, item.ApprovedPath); err != nil {
						fmt.Fprintf(s.writer, "    ❌ Error: %v\n", err)
					} else {
						fmt.Fprintf(s.writer, "    ✅ Copied\n")
						synced++
					}
				} else {
					fmt.Fprintf(s.writer, "    ⏭️  Skipped\n")
				}
			}

		case SyncUpdated:
			fmt.Fprintf(s.writer, "  Updated tool found:\n")
			fmt.Fprintf(s.writer, "    %s", item.Name)
			if item.Description != "" {
				fmt.Fprintf(s.writer, " - %q", item.Description)
			}
			fmt.Fprintln(s.writer)
			fmt.Fprintf(s.writer, "    Source: %s\n", item.StagingPath)
			s.printTimeoutWarning(item)
			fmt.Fprintf(s.writer, "    → Update %s? [y/N/d(iff)] ", item.ApprovedPath)

			if scanner.Scan() {
				answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if answer == "d" || answer == "diff" {
					s.showDiff(item.StagingPath, item.ApprovedPath)
					fmt.Fprintf(s.writer, "    → Update? [y/N] ")
					if scanner.Scan() {
						answer = strings.TrimSpace(strings.ToLower(scanner.Text()))
					}
				}
				if answer == "y" || answer == "yes" {
					if err := copyFile(item.StagingPath, item.ApprovedPath); err != nil {
						fmt.Fprintf(s.writer, "    ❌ Error: %v\n", err)
					} else {
						fmt.Fprintf(s.writer, "    ✅ Updated\n")
						synced++
					}
				} else {
					fmt.Fprintf(s.writer, "    ⏭️  Skipped\n")
				}
			}
		}
		fmt.Fprintln(s.writer)
	}

	return synced, nil
}

// compareFiles returns the sync status by comparing two files.
// If the approved file doesn't exist, returns SyncNew.
// If contents differ, returns SyncUpdated. Otherwise SyncUnchanged.
//
// compareFilesは2つのファイルを比較してsyncステータスを返します。
func compareFiles(stagingPath, approvedPath string) (SyncStatus, error) {
	approvedInfo, err := os.Stat(approvedPath)
	if os.IsNotExist(err) {
		return SyncNew, nil
	}
	if err != nil {
		return 0, err
	}

	stagingInfo, err := os.Stat(stagingPath)
	if err != nil {
		return 0, err
	}

	// Quick check: if sizes differ, files are different
	// クイックチェック: サイズが異なれば、ファイルは異なる
	if stagingInfo.Size() != approvedInfo.Size() {
		return SyncUpdated, nil
	}

	// Compare content using hashes
	// ハッシュを使用してコンテンツを比較
	stagingHash, err := fileHash(stagingPath)
	if err != nil {
		return 0, err
	}
	approvedHash, err := fileHash(approvedPath)
	if err != nil {
		return 0, err
	}

	if stagingHash != approvedHash {
		return SyncUpdated, nil
	}
	return SyncUnchanged, nil
}

// fileHash returns the SHA-256 hash of a file's content.
// fileHashはファイルコンテンツのSHA-256ハッシュを返します。
func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// copyFile copies a file from src to dst, preserving permissions.
// copyFileはsrcからdstにファイルをコピーし、権限を保持します。
func copyFile(src, dst string) error {
	// Ensure destination directory exists
	// 宛先ディレクトリが存在することを確認
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// writeProjectMeta writes the project metadata file in the approved directory.
// writeProjectMetaは承認済みディレクトリにプロジェクトメタデータファイルを書き込みます。
func writeProjectMeta(approvedDir, workspacePath string) error {
	meta := projectMeta{Workspace: workspacePath}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(approvedDir, projectMetaFile), data, 0644)
}

// printTimeoutWarning surfaces a tool's declared "@timeout:" value directly
// in the approval prompt, unconditionally (not behind the opt-in "d(iff)"
// command) — this is what actually delivers the safety property this
// feature depends on: a human approving `hostmcp tools sync` must see a
// non-default timeout declaration before typing "y", not just have the
// option to look for it via diff.
//
// printTimeoutWarningは、ツール自身が宣言した「@timeout:」の値を、承認
// プロンプトに無条件で（オプトインの「d(iff)」コマンドの裏に隠さず）
// 表示します — これが本機能の前提とする安全性を実際に担保するものです:
// `hostmcp tools sync`を承認する人間は、「y」と入力する前に既定値と異なる
// タイムアウト宣言を必ず目にする必要があり、diffで気づく機会があるだけでは
// 不十分です。
func (s *SyncManager) printTimeoutWarning(item SyncItem) {
	if item.Timeout <= 0 {
		return
	}
	if isJapaneseLocale() {
		fmt.Fprintf(s.writer, "    ⚠️  独自のタイムアウトを宣言: @timeout: %d秒", item.Timeout)
		switch {
		case s.cfg.MaxToolTimeout > 0 && item.Timeout > s.cfg.MaxToolTimeout:
			fmt.Fprintf(s.writer, "（max_tool_timeout: %d秒を超えるため %d秒にクランプされます）\n",
				s.cfg.MaxToolTimeout, s.cfg.MaxToolTimeout)
		default:
			fmt.Fprintf(s.writer, "（グローバル既定値は%d秒）\n", s.cfg.Timeout)
		}
		return
	}
	fmt.Fprintf(s.writer, "    ⚠️  Declares custom timeout: @timeout: %ds", item.Timeout)
	switch {
	case s.cfg.MaxToolTimeout > 0 && item.Timeout > s.cfg.MaxToolTimeout:
		fmt.Fprintf(s.writer, " (exceeds max_tool_timeout: %ds — will be clamped to %ds)\n",
			s.cfg.MaxToolTimeout, s.cfg.MaxToolTimeout)
	default:
		fmt.Fprintf(s.writer, " (global default is %ds)\n", s.cfg.Timeout)
	}
}

// isJapaneseLocale reports whether LC_ALL (falling back to LANG) indicates a
// Japanese locale, matching the same LC_ALL>LANG precedence and "ja_JP"
// prefix check used by isJapaneseLocale in internal/cli/client.go and
// writeSponsorMessage in internal/cli/serve.go. Duplicated here (rather than
// imported) because internal/cli already imports internal/hosttools, so the
// reverse import would create a cycle.
//
// isJapaneseLocaleは、LC_ALL（フォールバックでLANG）が日本語ロケールを示すかを
// 返します。internal/cli/client.goのisJapaneseLocaleやinternal/cli/serve.goの
// writeSponsorMessageと同じLC_ALL>LANGの優先順位、"ja_JP"接頭辞判定です。
// ここで重複定義しているのは（importせず）、internal/cliが既にinternal/hosttoolsを
// importしているため、逆方向のimportは循環依存になってしまうためです。
func isJapaneseLocale() bool {
	lang := os.Getenv("LC_ALL")
	if lang == "" {
		lang = os.Getenv("LANG")
	}
	return strings.HasPrefix(lang, "ja_JP")
}

// showDiff displays a simple line-by-line diff between two files.
// showDiffは2つのファイル間の簡易行単位diffを表示します。
func (s *SyncManager) showDiff(stagingPath, approvedPath string) {
	stagingData, err := os.ReadFile(stagingPath)
	if err != nil {
		fmt.Fprintf(s.writer, "    Error reading staging file: %v\n", err)
		return
	}
	approvedData, err := os.ReadFile(approvedPath)
	if err != nil {
		fmt.Fprintf(s.writer, "    Error reading approved file: %v\n", err)
		return
	}

	stagingLines := strings.Split(string(stagingData), "\n")
	approvedLines := strings.Split(string(approvedData), "\n")

	fmt.Fprintln(s.writer, "    --- approved (current)")
	fmt.Fprintln(s.writer, "    +++ staging (new)")

	maxLen := len(stagingLines)
	if len(approvedLines) > maxLen {
		maxLen = len(approvedLines)
	}

	for i := 0; i < maxLen; i++ {
		var sLine, aLine string
		if i < len(approvedLines) {
			aLine = approvedLines[i]
		}
		if i < len(stagingLines) {
			sLine = stagingLines[i]
		}
		if sLine != aLine {
			if i < len(approvedLines) {
				fmt.Fprintf(s.writer, "    - %s\n", aLine)
			}
			if i < len(stagingLines) {
				fmt.Fprintf(s.writer, "    + %s\n", sLine)
			}
		}
	}
}
