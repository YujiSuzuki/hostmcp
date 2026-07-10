// tools.go implements the 'tools' command group for managing host tools.
// Provides subcommands for syncing tools from staging to approved directory.
//
// tools.goはホストツール管理用の'tools'コマンドグループを実装します。
// ステージングから承認済みディレクトリへのツール同期のサブコマンドを提供します。
package cli

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/YujiSuzuki/hostmcp/internal/config"
	"github.com/YujiSuzuki/hostmcp/internal/hosttools"
)

// toolsCmd is the parent command for tool management subcommands.
// toolsCmdはツール管理サブコマンドの親コマンドです。
var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Manage host tools",
	Long:  `Manage host tools: sync staging tools to approved directory, list tools, etc.`,
}

// toolsSyncCmd syncs host tools from staging directories to the approved directory.
// toolsSyncCmdはステージングディレクトリから承認済みディレクトリにホストツールを同期します。
var toolsSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync host tools from staging to approved directory",
	Long: `Compare tools in staging directories (workspace) with approved tools,
and interactively approve new or updated tools for execution.

This command requires host_tools.approved_dir to be configured in hostmcp.yaml.
Tools in staging directories are proposals; only approved tools are executed.`,
	RunE: runToolsSync,
}

// toolsListCmd lists the approved directory path and project ID.
// toolsListCmdは承認済みディレクトリパスとプロジェクトIDを表示します。
var toolsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show approved tools and directories",
	Long:  `Show the approved tools directory, project ID, and list all approved tools.`,
	RunE:  runToolsList,
}

// flagToolsWorkspace overrides workspace_root for tools commands and, when
// --config is not given, is also used to derive the config file path.
// Mutually exclusive with --config; see resolveConfigFile.
var flagToolsWorkspace string

// flagToolsWorkspaceRoot overrides workspace_root for tools commands without
// affecting config file resolution. Unlike --workspace, it can be combined
// with --config — use it to reuse the same hostmcp.yaml across workspaces.
var flagToolsWorkspaceRoot string

func init() {
	rootCmd.AddCommand(toolsCmd)
	toolsCmd.AddCommand(toolsSyncCmd)
	toolsCmd.AddCommand(toolsListCmd)

	toolsCmd.PersistentFlags().StringVar(&flagToolsWorkspace, "workspace", "", "Workspace root directory; also derives the config file path (mutually exclusive with --config)")
	toolsCmd.PersistentFlags().StringVar(&flagToolsWorkspaceRoot, "workspace-root", "", "Override workspace_root only, without affecting config file resolution (combinable with --config)")
}

// runToolsSync performs an interactive sync of host tools.
// runToolsSyncはホストツールのインタラクティブな同期を実行します。
func runToolsSync(cmd *cobra.Command, args []string) error {
	resolvedConfig, err := resolveConfigFile(cfgFile, flagToolsWorkspace, "tools sync")
	if err != nil {
		return err
	}
	cfg, err := config.Load(resolvedConfig)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !cfg.HostAccess.HostTools.Enabled {
		return fmt.Errorf("host_tools is not enabled in configuration")
	}

	if !cfg.HostAccess.HostTools.IsSecureMode() {
		return fmt.Errorf("host_tools.approved_dir is not configured; sync requires secure mode")
	}

	workspaceRoot := cfg.HostAccess.WorkspaceRoot
	if flagToolsWorkspace != "" {
		workspaceRoot = flagToolsWorkspace
	}
	if flagToolsWorkspaceRoot != "" {
		workspaceRoot = flagToolsWorkspaceRoot
	}
	if workspaceRoot == "" {
		workspaceRoot = "."
	}
	absPath, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return fmt.Errorf("resolving workspace path: %w", err)
	}
	workspaceRoot = absPath

	// When --config was given directly (not derived from --workspace, and not
	// explicitly overridden via --workspace-root), workspace_root comes from the
	// config file and may not match where the operator expects it to point (e.g.
	// a relative workspace_root resolved against the current directory). Since
	// sync writes into the approved directory for this workspace, confirm it
	// before proceeding.
	if flagToolsWorkspace == "" && flagToolsWorkspaceRoot == "" {
		if !confirmWorkspaceRoot(workspaceRoot) {
			fmt.Println("Sync aborted.")
			return nil
		}
	}

	// Set up minimal logging
	handler := NewColoredHandler(os.Stdout, slog.LevelInfo)
	slog.SetDefault(slog.New(handler))

	syncMgr := hosttools.NewSyncManager(&cfg.HostAccess.HostTools, workspaceRoot)
	synced, err := syncMgr.RunInteractiveSync()
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	if synced > 0 {
		fmt.Printf("\n✅ %d tool(s) synced successfully.\n", synced)
	}

	return nil
}

// confirmWorkspaceRootInput is the source read by confirmWorkspaceRoot,
// overridable in tests to avoid blocking on the real stdin.
//
// confirmWorkspaceRootInputはconfirmWorkspaceRootが読み取る入力元です。
// 実際のstdinでブロックしないよう、テストでは上書きできます。
var confirmWorkspaceRootInput io.Reader = os.Stdin

// confirmWorkspaceRoot prints the resolved workspace root and asks the user to
// confirm it interactively, defaulting to "no" on empty input, EOF, or any
// answer other than y/yes. Used when workspace_root was not given explicitly
// via --workspace/--workspace-root (i.e. it came from the --config file as-is)
// and so may not be what the operator expects.
//
// confirmWorkspaceRootは解決済みのworkspace_rootを表示し、対話的に確認を求めます。
// 空入力・EOF・y/yes以外の入力はすべて「いいえ」として扱います。
// --workspace/--workspace-rootが指定されていない（つまりworkspace_rootが
// --configのファイルからそのまま来ている）場合に使用され、
// 操作者の意図と一致しない可能性があるworkspace_rootを確認します。
func confirmWorkspaceRoot(workspaceRoot string) bool {
	fmt.Printf("Workspace: %s\n", workspaceRoot)
	fmt.Print("Continue with this workspace? [y/N] ")

	scanner := bufio.NewScanner(confirmWorkspaceRootInput)
	if !scanner.Scan() {
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "y" || answer == "yes"
}

// runToolsList shows approved tools information.
// runToolsListは承認済みツール情報を表示します。
func runToolsList(cmd *cobra.Command, args []string) error {
	resolvedConfig, err := resolveConfigFile(cfgFile, flagToolsWorkspace, "tools list")
	if err != nil {
		return err
	}
	cfg, err := config.Load(resolvedConfig)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !cfg.HostAccess.HostTools.Enabled {
		return fmt.Errorf("host_tools is not enabled in configuration")
	}

	workspaceRoot := cfg.HostAccess.WorkspaceRoot
	if flagToolsWorkspace != "" {
		workspaceRoot = flagToolsWorkspace
	}
	if flagToolsWorkspaceRoot != "" {
		workspaceRoot = flagToolsWorkspaceRoot
	}
	if workspaceRoot == "" {
		workspaceRoot = "."
	}
	absPath, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return fmt.Errorf("resolving workspace path: %w", err)
	}
	workspaceRoot = absPath

	if cfg.HostAccess.HostTools.IsSecureMode() {
		projectID := hosttools.ProjectID(workspaceRoot)
		projectDir, _ := hosttools.ProjectApprovedDir(cfg.HostAccess.HostTools.ApprovedDir, workspaceRoot)
		commonDir, _ := hosttools.CommonApprovedDir(cfg.HostAccess.HostTools.ApprovedDir)

		fmt.Printf("Mode:         secure\n")
		fmt.Printf("Project ID:   %s\n", projectID)
		fmt.Printf("Workspace:    %s\n", workspaceRoot)
		fmt.Printf("Approved dir: %s\n", projectDir)
		if cfg.HostAccess.HostTools.Common {
			fmt.Printf("Common dir:   %s\n", commonDir)
		}
	} else {
		fmt.Printf("Mode:         legacy\n")
		fmt.Printf("Workspace:    %s\n", workspaceRoot)
		fmt.Printf("Directories:  %v\n", cfg.HostAccess.HostTools.Directories)
	}

	fmt.Println()

	// List tools
	mgr := hosttools.NewManager(&cfg.HostAccess.HostTools, workspaceRoot)
	tools, err := mgr.ListTools()
	if err != nil {
		fmt.Printf("No tools found: %v\n", err)
		return nil
	}

	if len(tools) == 0 {
		fmt.Println("No tools found.")
		return nil
	}

	fmt.Printf("Tools (%d):\n", len(tools))
	for _, tool := range tools {
		desc := tool.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Printf("  %-30s %s\n", tool.Name, desc)
	}

	return nil
}
