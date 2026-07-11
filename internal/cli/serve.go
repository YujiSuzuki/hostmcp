// serve.go implements the 'serve' command for starting the HostMCP MCP server.
// This file handles server configuration, logging setup, and graceful shutdown.
//
// serve.goはHostMCP MCPサーバーを起動する'serve'コマンドを実装します。
// このファイルはサーバー設定、ログ設定、グレースフルシャットダウンを処理します。
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/YujiSuzuki/hostmcp/internal/audit"
	"github.com/YujiSuzuki/hostmcp/internal/config"
	"github.com/YujiSuzuki/hostmcp/internal/docker"
	"github.com/YujiSuzuki/hostmcp/internal/hosttools"
	"github.com/YujiSuzuki/hostmcp/internal/mcp"
	"github.com/YujiSuzuki/hostmcp/internal/security"
)

var (
	// flagPort specifies the port number for the server to listen on.
	// When set, it overrides the port specified in the config file.
	//
	// flagPortはサーバーがリッスンするポート番号を指定します。
	// 設定された場合、設定ファイルのポートを上書きします。
	flagPort int

	// flagLogLevel specifies the logging verbosity level.
	// Valid values: debug, info, warn, error.
	//
	// flagLogLevelはログの詳細レベルを指定します。
	// 有効な値: debug, info, warn, error
	flagLogLevel string

	// flagHost specifies the host address to bind the server to.
	// When set, it overrides the host specified in the config file.
	//
	// flagHostはサーバーをバインドするホストアドレスを指定します。
	// 設定された場合、設定ファイルのホストを上書きします。
	flagHost string

	// flagLogFile specifies the path to the log file.
	// If empty, logs are written to stdout.
	//
	// flagLogFileはログファイルへのパスを指定します。
	// 空の場合、ログはstdoutに出力されます。
	flagLogFile string

	// flagLogAlsoStdout enables dual logging to both file and stdout.
	// Only effective when flagLogFile is set.
	//
	// flagLogAlsoStdoutはファイルとstdoutの両方へのログ出力を有効にします。
	// flagLogFileが設定されている場合のみ有効です。
	flagLogAlsoStdout bool

	// flagAllowExec holds temporary exec whitelist entries specified via CLI.
	// Format: "container:command" (e.g., "myapp:npm test").
	// These entries are only valid for the current server session.
	//
	// flagAllowExecはCLIで指定された一時的なexecホワイトリストエントリを保持します。
	// フォーマット: "container:command"（例: "myapp:npm test"）
	// これらのエントリは現在のサーバーセッションでのみ有効です。
	flagAllowExec []string

	// flagDangerously specifies containers to enable dangerous mode for.
	// Format: comma-separated container names (e.g., "api,web").
	// Enables exec_dangerously for the specified containers with default commands.
	//
	// flagDangerouslyは危険モードを有効にするコンテナを指定します。
	// フォーマット: カンマ区切りのコンテナ名（例: "api,web"）
	// 指定されたコンテナに対してデフォルトコマンドでexec_dangerouslyを有効にします。
	flagDangerously string

	// flagDangerouslyAll enables dangerous mode for all containers.
	// When set, all containers can use dangerous commands with default command list.
	//
	// flagDangerouslyAllは全コンテナに対して危険モードを有効にします。
	// 設定された場合、全コンテナがデフォルトコマンドリストで危険コマンドを使用できます。
	flagDangerouslyAll bool

	// flagWorkspace specifies the host-side workspace root directory.
	// Used as the working directory for host commands and tool discovery base.
	// Overrides host_access.workspace_root and blocked_paths.auto_import.workspace_root in config.
	// Also used to derive the config file path when --config is not given.
	// Mutually exclusive with --config; see resolveConfigFile.
	//
	// flagWorkspaceはホスト側のワークスペースルートディレクトリを指定します。
	// ホストコマンドの作業ディレクトリおよびツール検出の基点として使用されます。
	// 設定ファイルのhost_access.workspace_rootおよびblocked_paths.auto_import.workspace_rootを上書きします。
	// --configが指定されていない場合、設定ファイルパスの導出にも使用されます。
	// --configとは併用不可（resolveConfigFile参照）。
	flagWorkspace string

	// flagWorkspaceRoot overrides host_access.workspace_root (and the derived
	// blocked_paths.auto_import.workspace_root) without affecting config file
	// resolution. Unlike --workspace, it can be combined with --config — use it to
	// reuse the same hostmcp.yaml (security policy, command whitelist, etc.) while
	// pointing at a different workspace directory.
	//
	// flagWorkspaceRootは設定ファイルの解決には関与せず、host_access.workspace_root
	// （および派生するblocked_paths.auto_import.workspace_root）のみを上書きします。
	// --workspaceと異なり--configと併用可能です。同じhostmcp.yaml
	// （セキュリティポリシー・コマンドホワイトリスト等）を使い回しつつ、
	// 別のワークスペースディレクトリを指定したい場合に使用します。
	flagWorkspaceRoot string

	// flagHostDangerously enables dangerous mode for host commands.
	// When set, host commands in the dangerously list can be executed
	// with the dangerously=true parameter.
	//
	// flagHostDangerouslyはホストコマンドの危険モードを有効にします。
	// 設定された場合、dangerouslyリストのホストコマンドが
	// dangerously=trueパラメータで実行可能になります。
	flagHostDangerously bool

	// flagVerbosity controls the verbosity level for logging.
	// Level 0: Normal (INFO level, minimal output)
	// Level 1 (-v): Verbose (INFO + JSON for initialized clients)
	// Level 2 (-vv): More verbose (DEBUG + JSON, filter noise)
	// Level 3 (-vvv): Full debug (DEBUG + all JSON, show noise)
	// Level 4 (-vvvv): Full debug + HTTP headers
	//
	// flagVerbosityはログの詳細レベルを制御します。
	// レベル0: 通常（INFOレベル、最小出力）
	// レベル1 (-v): 詳細（INFO + 初期化済みクライアントのJSON）
	// レベル2 (-vv): より詳細（DEBUG + JSON、ノイズをフィルタ）
	// レベル3 (-vvv): フルデバッグ（DEBUG + 全JSON、ノイズも表示）
	// レベル4 (-vvvv): フルデバッグ + HTTPヘッダー表示
	flagVerbosity int

	// flagSync enables tool sync check before starting the server.
	// When set, compares staging directories with approved directory
	// and prompts the user to approve new or updated tools.
	//
	// flagSyncはサーバー起動前のツール同期チェックを有効にします。
	// 設定すると、ステージングディレクトリと承認済みディレクトリを比較し、
	// 新しいまたは更新されたツールの承認をユーザーに求めます。
	flagSync bool

	// flagDev enables development mode for host tools.
	// In dev mode, staging directories are included with highest priority,
	// allowing tools under development to be tested without approval.
	// Only effective in secure mode (approved_dir is set).
	//
	// flagDevはホストツールの開発モードを有効にします。
	// 開発モードでは、ステージングディレクトリが最優先で読み込まれ、
	// 承認なしで開発中のツールをテストできます。
	// セキュアモード（approved_dirが設定済み）でのみ有効です。
	flagDev bool

	// flagNoThanks hides the sponsor message at server startup.
	//
	// flagNoThanksはサーバー起動時のスポンサーメッセージを非表示にします。
	flagNoThanks bool
)

// serveCmd represents the 'serve' command that starts the MCP server.
// The server provides HTTP/SSE endpoints for AI assistants to interact with Docker.
//
// serveCmdはMCPサーバーを起動する'serve'コマンドを表します。
// サーバーはAIアシスタントがDockerと対話するためのHTTP/SSEエンドポイントを提供します。
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the MCP server",
	Long: `Start the HostMCP server to provide Docker container access
to AI assistants through HTTP/SSE protocol.`,
	RunE: runServe,
}

// init registers the serve command and its flags.
// This function is automatically called when the package is imported.
//
// initはserveコマンドとそのフラグを登録します。
// この関数はパッケージがインポートされたときに自動的に呼び出されます。
func init() {
	// Add serve as a subcommand of the root command.
	// serveをルートコマンドのサブコマンドとして追加します。
	rootCmd.AddCommand(serveCmd)

	// Add flags that override config file settings.
	// These flags allow runtime configuration without modifying the config file.
	//
	// 設定ファイルの設定を上書きするフラグを追加します。
	// これらのフラグにより、設定ファイルを変更せずにランタイム設定が可能です。
	serveCmd.Flags().IntVar(&flagPort, "port", 0, "Port to listen on (overrides config)")
	serveCmd.Flags().StringVar(&flagLogLevel, "log-level", "", "Log level: debug, info, warn, error (overrides config)")
	serveCmd.Flags().StringVar(&flagHost, "host", "", "Host to bind to (overrides config)")
	serveCmd.Flags().StringVar(&flagLogFile, "log-file", "", "Log file path (default: stdout, set to enable file logging)")
	serveCmd.Flags().BoolVar(&flagLogAlsoStdout, "log-also-stdout", false, "Also log to stdout when log-file is set")
	serveCmd.Flags().StringArrayVar(&flagAllowExec, "allow-exec", []string{}, "Temporarily allow exec command (format: container:command)")

	// Add flags for dangerous mode
	// 危険モード用のフラグを追加
	serveCmd.Flags().StringVar(&flagDangerously, "dangerously", "", "Enable dangerous mode for specific containers (comma-separated, e.g., 'api,web')")
	serveCmd.Flags().BoolVar(&flagDangerouslyAll, "dangerously-all", false, "Enable dangerous mode for all containers with default commands (tail, cat, grep, head, less, wc, ls)")

	// Add verbosity flag for detailed logging (-v, -vv, -vvv)
	// 詳細ログ用の詳細モードフラグを追加（-v, -vv, -vvv）
	serveCmd.Flags().CountVarP(&flagVerbosity, "verbose", "v", "Increase verbosity level (-v: JSON output, -vv: debug level, -vvv: full debug with noise, -vvvv: + HTTP headers)")

	// Add host access flags
	// ホストアクセスフラグを追加
	serveCmd.Flags().StringVar(&flagWorkspace, "workspace", "", "Host workspace root directory; also derives the config file path (mutually exclusive with --config)")
	serveCmd.Flags().StringVar(&flagWorkspaceRoot, "workspace-root", "", "Override host_access.workspace_root only, without affecting config file resolution (combinable with --config)")
	serveCmd.Flags().BoolVar(&flagHostDangerously, "host-dangerously", false, "Enable dangerous mode for host commands")

	// Add sync flag for host tools
	// ホストツール用の同期フラグを追加
	serveCmd.Flags().BoolVar(&flagSync, "sync", false, "Sync host tools from staging to approved directory before starting")

	// Add dev flag for host tools development
	// ホストツール開発用のdevフラグを追加
	serveCmd.Flags().BoolVar(&flagDev, "dev", false, "Development mode: also load tools from staging directories (staging > approved > common)")

	// Add sponsor message flag
	// スポンサーメッセージ用フラグを追加
	serveCmd.Flags().BoolVar(&flagNoThanks, "no-thanks", false, "Hide sponsor message at startup")
}

// runServe is the main entry point for the serve command.
// It initializes all components and starts the MCP server.
//
// runServeはserveコマンドのメインエントリーポイントです。
// すべてのコンポーネントを初期化し、MCPサーバーを起動します。
func runServe(cmd *cobra.Command, args []string) error {
	// Resolve --config / --workspace flags to a config file path first,
	// so that flag errors are reported before any cosmetic output.
	// バナー表示前にフラグを解決し、フラグエラーは cosmetic 出力の前に返す。
	resolvedCfg, err := resolveConfigFile(cfgFile, flagWorkspace, "serve")
	if err != nil {
		return err
	}

	// Show banner and sponsor message before log output.
	// ログ出力の前にバナーとスポンサーメッセージを表示します。
	showBanner()
	showSponsorMessage()

	// Load configuration from file.
	// 設定ファイルから設定を読み込みます。
	cfg, err := config.Load(resolvedCfg)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Override config with command-line flags.
	// CLI flags take precedence over config file settings.
	//
	// コマンドラインフラグで設定を上書きします。
	// CLIフラグは設定ファイルの設定よりも優先されます。
	if flagPort > 0 {
		cfg.Server.Port = flagPort
	}
	if flagHost != "" {
		cfg.Server.Host = flagHost
	}
	if flagLogLevel != "" {
		cfg.Logging.Level = flagLogLevel
	}

	// Override log level based on verbosity level
	// verbosityレベルに基づいてログレベルを上書き
	// -vv, -vvv, -vvvv set log level to debug
	// -vv以上はログレベルをdebugに設定
	if flagVerbosity >= 2 {
		cfg.Logging.Level = "debug"
	}

	// Parse and set the log level based on configuration.
	// Convert string level (debug/info/warn/error) to slog.Level.
	//
	// 設定に基づいてログレベルを解析・設定します。
	// 文字列レベル（debug/info/warn/error）をslog.Levelに変換します。
	var logLevel slog.Level
	switch cfg.Logging.Level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	// Configure log output destination.
	// Supports file logging (plain text), stdout (colored), or both simultaneously.
	//
	// ログ出力先を設定します。
	// ファイルログ（プレーンテキスト）、stdout（カラー）、または両方への同時出力をサポートします。
	if flagLogFile != "" {
		// Open log file for appending (create if doesn't exist).
		// ログファイルを追記モードで開きます（存在しない場合は作成）。
		f, err := os.OpenFile(flagLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}
		defer f.Close()

		if flagLogAlsoStdout {
			// Dual output: colored for stdout, plain text for file.
			// Create a multi-handler that routes to both outputs.
			//
			// デュアル出力: stdoutはカラー、ファイルはプレーンテキスト。
			// 両方の出力にルーティングするマルチハンドラーを作成します。
			coloredHandler := NewColoredHandler(os.Stdout, logLevel)
			fileHandler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: logLevel})
			logger := slog.New(&multiHandler{handlers: []slog.Handler{coloredHandler, fileHandler}})
			slog.SetDefault(logger)
		} else {
			// File only: plain text output.
			// ファイルのみ: プレーンテキスト出力。
			handler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: logLevel})
			logger := slog.New(handler)
			slog.SetDefault(logger)
		}
	} else {
		// Default: colored log to stdout with the configured level.
		// デフォルト: 設定されたレベルでstdoutにカラーログを出力します。
		handler := NewColoredHandler(os.Stdout, logLevel)
		logger := slog.New(handler)
		slog.SetDefault(logger)
	}

	// Apply workspace path settings: resolve flag overrides, propagate to AutoImport, ensure absolute paths.
	// ワークスペースパス設定の適用：フラグの上書き、AutoImportへの伝播、絶対パスの保証。
	if err := applyWorkspaceOverrides(cfg, flagWorkspace, flagWorkspaceRoot); err != nil {
		return err
	}

	// When --config was given directly (not derived from --workspace, and not
	// explicitly overridden via --workspace-root), workspace_root came from the
	// config file as-is and may not be what the operator expects (e.g. a relative
	// workspace_root resolved against the current directory rather than the
	// config file's location). Surface this rather than silently proceeding.
	// --configが直接指定された場合（--workspaceからの導出でも--workspace-rootでの
	// 明示的な上書きでもない場合）、workspace_rootは設定ファイルの値がそのまま
	// 使われており、操作者の想定と異なる可能性があります（例：相対パスの
	// workspace_rootが設定ファイルの場所ではなくカレントディレクトリ基準で解決される）。
	// 黙って進めず、この状況を可視化します。
	if flagWorkspace == "" && flagWorkspaceRoot == "" {
		logWorkspaceConfigLocation(resolvedCfg, cfg.HostAccess.WorkspaceRoot)
	}

	// Apply --host-dangerously flag to enable dangerous mode for host commands
	// --host-dangerouslyフラグでホストコマンドの危険モードを有効化
	if flagHostDangerously {
		cfg.HostAccess.HostCommands.Dangerously.Enabled = true
	}

	// Parse and apply --allow-exec flags for temporary command whitelisting.
	// --allow-execフラグを解析して一時的なコマンドホワイトリストを適用します。
	if err := applyAllowExecFlags(cfg, flagAllowExec); err != nil {
		return err
	}

	// Parse and apply --dangerously and --dangerously-all flags.
	// --dangerouslyおよび--dangerously-allフラグを解析して適用します。
	if err := applyDangerouslyFlags(cfg, flagDangerously, flagDangerouslyAll); err != nil {
		return err
	}

	// Initialize audit logging (rotates previous log file on startup).
	// 監査ログを初期化します（起動時に前回のログファイルをローテーション）。
	if cfg.Audit.Enabled {
		if err := audit.Initialize(cfg.Audit); err != nil {
			return fmt.Errorf("audit logging: %w", err)
		}
		defer audit.ResetLogger()
		slog.Info("Audit logging enabled", "file", audit.GetLogger().FilePath(), "keep", cfg.Audit.Rotation.Keep)
	}

	// Log server startup information.
	// サーバー起動情報をログに出力します。
	slog.Info("Starting HostMCP server",
		"version", Version,
		"security_mode", cfg.Security.Mode,
		"port", cfg.Server.Port,
		"log_level", cfg.Logging.Level,
	)

	// Log verbosity level if set.
	// verbosityレベルが設定されている場合、ログに出力します。
	if flagVerbosity > 0 {
		verbosityDesc := []string{
			"",
			"-v: JSON output enabled, noise filtered",
			"-vv: DEBUG level, JSON output enabled, noise filtered",
			"-vvv: DEBUG level, full JSON output, all connections shown",
		}
		level := flagVerbosity
		if level > 3 {
			level = 3
		}
		slog.Info("Verbosity mode enabled", "level", flagVerbosity, "description", verbosityDesc[level])
	}

	// Create security policy from configuration.
	// The policy enforces container access rules and command whitelisting.
	//
	// 設定からセキュリティポリシーを作成します。
	// ポリシーはコンテナアクセスルールとコマンドホワイトリストを適用します。
	policy := security.NewPolicy(&cfg.Security)

	// Create Docker client with the security policy.
	// All Docker operations will be validated against this policy.
	//
	// セキュリティポリシーでDockerクライアントを作成します。
	// すべてのDocker操作はこのポリシーに対して検証されます。
	dockerClient, err := docker.NewClient(policy)
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer dockerClient.Close()

	// Check accessible containers at startup.
	// This helps verify the security policy is working correctly.
	//
	// 起動時にアクセス可能なコンテナを確認します。
	// これによりセキュリティポリシーが正しく動作しているか確認できます。
	ctx := context.Background()
	containers, err := dockerClient.ListContainers(ctx)
	if err != nil {
		slog.Warn("Failed to list containers at startup", "error", err)
	} else {
		if len(containers) == 0 {
			// Warn if no containers match the allowed patterns.
			// This might indicate a misconfiguration.
			//
			// 許可パターンに一致するコンテナがない場合は警告します。
			// これは設定ミスを示している可能性があります。
			slog.Warn("No accessible containers found matching the allowed patterns",
				"patterns", cfg.Security.AllowedContainers)
		} else {
			// Log the count and details of accessible containers.
			// アクセス可能なコンテナの数と詳細をログに出力します。
			slog.Info("Found accessible containers",
				"count", len(containers))
			for _, container := range containers {
				slog.Debug("Accessible container",
					"name", container.Name,
					"id", container.ID[:12],
					"status", container.Status)
			}
		}
	}

	// Initialize blocked paths with container names.
	// Blocked paths prevent access to sensitive files like secrets and .env.
	//
	// コンテナ名でブロックパスを初期化します。
	// ブロックパスはsecretsや.envのような機密ファイルへのアクセスを防ぎます。
	var containerNames []string
	for _, c := range containers {
		containerNames = append(containerNames, c.Name)
	}
	if err := dockerClient.InitBlockedPaths(containerNames); err != nil {
		slog.Warn("Failed to initialize blocked paths", "error", err)
	}

	// Create MCP server with the Docker client.
	// The server handles HTTP/SSE requests from AI assistants.
	// Pass verbosity level to control logging behavior.
	//
	// DockerクライアントでMCPサーバーを作成します。
	// サーバーはAIアシスタントからのHTTP/SSEリクエストを処理します。
	// ログ動作を制御するためにverbosityレベルを渡します。

	// Set MCP server version to match CLI version
	// MCPサーバーバージョンをCLIバージョンに合わせて設定
	mcp.ServerVersion = Version

	var serverOpts []mcp.ServerOption
	if flagVerbosity > 0 {
		serverOpts = append(serverOpts, mcp.WithVerbosity(flagVerbosity))
	}

	// Configure host tools if enabled
	// ホストツールが有効な場合は設定
	if cfg.HostAccess.HostTools.Enabled {
		// Run sync if --sync flag is set and secure mode is configured
		// --syncフラグが設定されていてセキュアモードが構成されている場合に同期を実行
		if flagSync && cfg.HostAccess.HostTools.IsSecureMode() {
			syncMgr := hosttools.NewSyncManager(&cfg.HostAccess.HostTools, cfg.HostAccess.WorkspaceRoot)
			synced, err := syncMgr.RunInteractiveSync()
			if err != nil {
				return fmt.Errorf("host tools sync failed: %w", err)
			}
			if synced > 0 {
				slog.Info("Host tools synced", "count", synced)
			}
		} else if flagSync && !cfg.HostAccess.HostTools.IsSecureMode() {
			slog.Warn("--sync flag ignored: host_tools.approved_dir is not configured (legacy mode)")
		}

		htManager := hosttools.NewManager(&cfg.HostAccess.HostTools, cfg.HostAccess.WorkspaceRoot)

		// Enable dev mode if --dev flag is set and secure mode is configured
		// --devフラグが設定されていてセキュアモードが構成されている場合に開発モードを有効化
		if flagDev && cfg.HostAccess.HostTools.IsSecureMode() {
			htManager.SetDevMode(true)
			slog.Warn("Development mode: staging tools are directly executable (not approved)",
				"staging_dirs", cfg.HostAccess.HostTools.StagingDirs,
			)
		} else if flagDev && !cfg.HostAccess.HostTools.IsSecureMode() {
			slog.Warn("--dev flag ignored: host_tools.approved_dir is not configured (legacy mode)")
		}

		serverOpts = append(serverOpts, mcp.WithHostToolsManager(htManager))

		if cfg.HostAccess.HostTools.IsSecureMode() {
			projectDir, _ := hosttools.ProjectApprovedDir(cfg.HostAccess.HostTools.ApprovedDir, cfg.HostAccess.WorkspaceRoot)
			slog.Info("Host tools enabled (secure mode)",
				"approved_dir", projectDir,
				"staging_dirs", cfg.HostAccess.HostTools.StagingDirs,
				"common", cfg.HostAccess.HostTools.Common,
				"extensions", cfg.HostAccess.HostTools.AllowedExtensions,
			)
		} else {
			slog.Info("Host tools enabled (legacy mode)",
				"workspace", cfg.HostAccess.WorkspaceRoot,
				"directories", cfg.HostAccess.HostTools.Directories,
				"extensions", cfg.HostAccess.HostTools.AllowedExtensions,
			)
		}
	}

	// Configure host commands if enabled
	// ホストコマンドが有効な場合は設定
	if cfg.HostAccess.HostCommands.Enabled {
		hcPolicy := security.NewHostCommandPolicy(&cfg.HostAccess.HostCommands)
		timeout := time.Duration(cfg.HostAccess.HostTools.Timeout) * time.Second
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		serverOpts = append(serverOpts, mcp.WithHostCommandPolicy(hcPolicy, cfg.HostAccess.WorkspaceRoot, timeout))
		slog.Info("Host commands enabled",
			"workspace", cfg.HostAccess.WorkspaceRoot,
			"dangerously", cfg.HostAccess.HostCommands.Dangerously.Enabled,
		)
	}

	mcpServer := mcp.NewServer(dockerClient, cfg.Server.Port, serverOpts...)

	// Start server in a goroutine for non-blocking operation.
	// Errors are sent to errChan for handling in the main goroutine.
	//
	// ノンブロッキング操作のためにゴルーチンでサーバーを起動します。
	// エラーはメインゴルーチンで処理するためにerrChanに送信されます。
	errChan := make(chan error, 1)
	go func() {
		if err := mcpServer.Start(); err != nil {
			errChan <- err
		}
	}()

	// Log the server endpoints for user reference.
	// サーバーエンドポイントをユーザー参照用にログに出力します。
	addr := cfg.GetAddress()
	slog.Info("MCP server listening",
		"url", fmt.Sprintf("http://%s", addr),
		"health_check", fmt.Sprintf("http://%s/health", addr),
		"sse_endpoint", fmt.Sprintf("http://%s/sse", addr),
	)
	slog.Info("Press Ctrl+C to stop")

	// Wait for interrupt signal (Ctrl+C) or SIGTERM.
	// This enables graceful shutdown of the server.
	//
	// 割り込みシグナル（Ctrl+C）またはSIGTERMを待機します。
	// これによりサーバーのグレースフルシャットダウンが可能になります。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Wait for either server error or shutdown signal.
	// サーバーエラーまたはシャットダウンシグナルを待機します。
	select {
	case err := <-errChan:
		// Server encountered an error during operation.
		// サーバー動作中にエラーが発生しました。
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		// Received shutdown signal, initiate graceful shutdown.
		// シャットダウンシグナルを受信、グレースフルシャットダウンを開始します。
		slog.Info("Shutting down gracefully...")

		// Allow up to 10 seconds for graceful shutdown.
		// グレースフルシャットダウンに最大10秒を許可します。
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := mcpServer.Stop(shutdownCtx); err != nil {
			return fmt.Errorf("failed to stop server: %w", err)
		}
		slog.Info("Server stopped")
	}

	return nil
}

// resolveConfigFile resolves the config file path from --config and --workspace flags.
// If cfgFile is already set (via --config), it is returned unchanged.
// If --config was not given, the config path is derived from --workspace as:
//
//	{absWorkspace}/.sandbox/config/hostmcp.yaml
//
// cmdName is the invoking command's usage string (e.g. "serve", "tools list"),
// used only to build accurate usage examples in the "neither flag given" error.
//
// Returns an error if neither flag is given, the path cannot be resolved, or the file
// cannot be accessed (with distinct messages for "not found" vs other errors).
//
// resolveConfigFileは--configと--workspaceフラグから設定ファイルパスを解決します。
// --configが指定済みの場合はそのまま返します。
// --configが指定されていない場合、設定パスを--workspaceから次のように導出します：
//
//	{absWorkspace}/.sandbox/config/hostmcp.yaml
//
// cmdNameは呼び出し元コマンドの使用文字列（例: "serve"、"tools list"）で、
// 「どちらのフラグも未指定」エラーの使用例を正しく組み立てるためだけに使われます。
func resolveConfigFile(cfgFile, flagWorkspace, cmdName string) (string, error) {
	if cfgFile == "" && flagWorkspace == "" {
		return "", fmt.Errorf(
			"either --config or --workspace is required\n\n"+
				"  hostmcp %s --workspace /path/to/project\n"+
				"      Uses {workspace}/.sandbox/config/hostmcp.yaml\n\n"+
				"  hostmcp %s --config /path/to/hostmcp.yaml\n"+
				"      Uses the specified config file directly",
			cmdName, cmdName,
		)
	}
	if cfgFile != "" && flagWorkspace != "" {
		return "", fmt.Errorf(
			"--config and --workspace are mutually exclusive\n\n"+
				"  hostmcp %s --workspace /path/to/project\n"+
				"      Uses {workspace}/.sandbox/config/hostmcp.yaml\n\n"+
				"  hostmcp %s --config /path/to/hostmcp.yaml\n"+
				"      Uses the specified config file directly\n\n"+
				"To override workspace_root while using --config, use --workspace-root instead.",
			cmdName, cmdName,
		)
	}
	if cfgFile != "" {
		return cfgFile, nil
	}

	// --config was not given; derive config path from --workspace.
	// --config が指定されていない場合、--workspace から設定ファイルパスを決定します。
	absWorkspace, err := filepath.Abs(flagWorkspace)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace path %q: %w", flagWorkspace, err)
	}
	resolved := filepath.Join(absWorkspace, ".sandbox", "config", "hostmcp.yaml")
	if _, err := os.Stat(resolved); errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf(
			".sandbox/config/hostmcp.yaml not found in %s\n\nRun: hostmcp init --workspace %s",
			absWorkspace, absWorkspace,
		)
	} else if err != nil {
		return "", fmt.Errorf("failed to access config file %s: %w", resolved, err)
	}
	return resolved, nil
}

// logWorkspaceConfigLocation logs the resolved workspace_root, warning instead
// of informing when the config file's path does not match the standard
// {workspace}/.sandbox/config/hostmcp.yaml layout implied by workspace_root —
// a sign the two may have diverged unintentionally. If workspace_root is
// empty (valid when host_access.host_commands is disabled), the config path
// cannot be resolved, or it doesn't follow that layout at all, divergence
// can't be determined, so this only logs the workspace at info level.
//
// Only meaningful when --config was given directly; see the call site.
//
// logWorkspaceConfigLocationは解決済みのworkspace_rootをログ出力します。
// 設定ファイルのパスがworkspace_rootから導出される標準レイアウト
// {workspace}/.sandbox/config/hostmcp.yamlと一致しない場合は、意図しない
// 乖離の兆候としてwarningを出します。workspace_rootが空（host_access.host_commandsが
// 無効な場合は正当な状態）、設定ファイルのパスが解決できない、または
// そもそもそのレイアウトに従っていない場合は乖離を判定できないため、
// workspaceをinfoレベルで出力するのみです。
//
// --configが直接指定された場合にのみ意味を持ちます。呼び出し箇所を参照。
func logWorkspaceConfigLocation(resolvedCfg, workspaceRoot string) {
	if workspaceRoot == "" {
		// Not configured (valid when host_access.host_commands is disabled),
		// so there's nothing to compare the config location against.
		slog.Info("using workspace", "workspace", workspaceRoot)
		return
	}

	absCfg, err := filepath.Abs(resolvedCfg)
	if err != nil {
		slog.Info("using workspace", "workspace", workspaceRoot)
		return
	}

	dir := filepath.Dir(absCfg)
	if filepath.Base(absCfg) != "hostmcp.yaml" || filepath.Base(dir) != "config" || filepath.Base(filepath.Dir(dir)) != ".sandbox" {
		// Config doesn't follow the {workspace}/.sandbox/config/hostmcp.yaml
		// layout at all, so there's no implied workspace to compare against.
		slog.Info("using workspace", "workspace", workspaceRoot)
		return
	}

	impliedWorkspace := filepath.Dir(filepath.Dir(dir))
	if impliedWorkspace != filepath.Clean(workspaceRoot) {
		slog.Warn("config file location and workspace_root appear to have diverged",
			"config", absCfg,
			"workspace_root", workspaceRoot,
			"workspace_implied_by_config_location", impliedWorkspace,
		)
		return
	}

	slog.Info("using workspace", "workspace", workspaceRoot)
}

// showBanner displays the ASCII art banner to stdout.
//
// showBannerはASCIIアートバナーをstdoutに表示します。
func showBanner() {
	writeBanner(os.Stdout)
}

// writeBanner writes the ASCII art banner to the given writer.
//
// writeBannerは指定されたwriterにASCIIアートバナーを書き込みます。
func writeBanner(w io.Writer) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "   _   ___   ___               _ _")
	fmt.Fprintln(w, `  /_\ |_ _| / __| __ _ _ _  _| | |__  _____ __`)
	fmt.Fprintln(w, ` / _ \ | |  \__ \/ _`+"`"+` | ' \/ _`+"`"+` | '_ \/ _ \ \ /`)
	fmt.Fprintln(w, `/_/ \_\___| |___/\__,_|_||_\__,_|_.__/\___/_\_\`)
	fmt.Fprintln(w)
	fmt.Fprintln(w, ` _   _              _     __  __    ___   ___ `)
	fmt.Fprintln(w, `| |_| |  ___   ___ | |_  |  \/  |  / __| | _ \`)
	fmt.Fprintln(w, `| ___ | / _ \ (_-< |  _| | |\/| | | (__  |  _/`)
	fmt.Fprintln(w, `|_| |_| \___/ /__/  \__| |_|  |_|  \___| |_|  `)
	fmt.Fprintf(w, "                         %s\n", Version)
}

// showSponsorMessage displays a GitHub Sponsors message to stdout.
// Delegates to writeSponsorMessage which handles --no-thanks suppression.
//
// showSponsorMessageはGitHub Sponsorsメッセージをstdoutに表示します。
// --no-thanksによる抑制はwriteSponsorMessageで処理されます。
func showSponsorMessage() {
	writeSponsorMessage(os.Stdout)
}

// writeSponsorMessage writes the sponsor message to the given writer.
// Returns true if the message was written, false if suppressed by --no-thanks.
//
// writeSponsorMessageは指定されたwriterにスポンサーメッセージを書き込みます。
// メッセージが書き込まれた場合はtrue、--no-thanksで抑制された場合はfalseを返します。
func writeSponsorMessage(w io.Writer) bool {
	if flagNoThanks {
		return false
	}

	const sponsorURL = "https://github.com/sponsors/YujiSuzuki"

	lang := os.Getenv("LC_ALL")
	if lang == "" {
		lang = os.Getenv("LANG")
	}

	fmt.Fprintln(w)
	if strings.HasPrefix(lang, "ja_JP") {
		fmt.Fprintln(w, "💖 このプロジェクトを応援")
		fmt.Fprintln(w, "  HostMCP が役に立ったら、スポンサーになって応援してください！")
		fmt.Fprintf(w, "  %s\n", sponsorURL)
	} else {
		fmt.Fprintln(w, "💖 Support this project")
		fmt.Fprintln(w, "  If you find HostMCP useful, consider sponsoring!")
		fmt.Fprintf(w, "  %s\n", sponsorURL)
	}
	fmt.Fprintln(w)
	return true
}

// applyWorkspaceOverrides resolves and propagates workspace path settings into cfg.
// It applies four transformations in order:
//  1. If flagWorkspace is non-empty, it overrides both HostAccess.WorkspaceRoot and
//     AutoImport.WorkspaceRoot (--workspace flag takes precedence over config file).
//  2. If flagWorkspaceRoot is non-empty, it overrides both fields again, taking
//     precedence over flagWorkspace. Unlike --workspace, --workspace-root only
//     overrides workspace_root and never affects config file resolution, so it can
//     be combined with --config (which is mutually exclusive with --workspace).
//  3. If HostAccess.WorkspaceRoot is set, it is resolved to an absolute path and
//     propagated to AutoImport.WorkspaceRoot.
//  4. If AutoImport.WorkspaceRoot is still relative (e.g. the default "."), it is
//     resolved to an absolute path to eliminate CWD dependency.
//
// applyWorkspaceOverridesはワークスペースパス設定を解決してcfgに伝播します。
// 以下の4つの変換を順に適用します：
//  1. flagWorkspaceが空でない場合、HostAccess.WorkspaceRootとAutoImport.WorkspaceRoot
//     の両方を上書き（--workspaceフラグは設定ファイルより優先）。
//  2. flagWorkspaceRootが空でない場合、両フィールドをさらに上書き（flagWorkspaceより優先）。
//     --workspaceと異なり、--workspace-rootはworkspace_rootの上書きのみを行い、設定ファイルの
//     解決には関与しないため、（--workspaceとは併用不可の）--configと組み合わせられます。
//  3. HostAccess.WorkspaceRootが設定されている場合、絶対パスに解決し
//     AutoImport.WorkspaceRootにも伝播。
//  4. AutoImport.WorkspaceRootがまだ相対パス（デフォルト"."など）の場合、
//     CWD依存を排除するために絶対パスに変換。
func applyWorkspaceOverrides(cfg *config.Config, flagWorkspace, flagWorkspaceRoot string) error {
	// Step 1: CLI flag takes precedence over config file.
	// Step 1: CLIフラグは設定ファイルより優先される。
	if flagWorkspace != "" {
		cfg.HostAccess.WorkspaceRoot = flagWorkspace
		cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot = flagWorkspace
	}

	// Step 2: --workspace-root takes precedence over --workspace and the config file.
	// Step 2: --workspace-rootは--workspaceおよび設定ファイルより優先される。
	if flagWorkspaceRoot != "" {
		cfg.HostAccess.WorkspaceRoot = flagWorkspaceRoot
		cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot = flagWorkspaceRoot
	}

	// Step 3: Resolve HostAccess.WorkspaceRoot and propagate to AutoImport.
	// Step 3: HostAccess.WorkspaceRootを解決してAutoImportに伝播。
	if cfg.HostAccess.WorkspaceRoot != "" {
		absPath, err := filepath.Abs(cfg.HostAccess.WorkspaceRoot)
		if err != nil {
			return fmt.Errorf("failed to resolve workspace path %q: %w", cfg.HostAccess.WorkspaceRoot, err)
		}
		cfg.HostAccess.WorkspaceRoot = absPath
		cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot = absPath
	}

	// Step 4: Ensure AutoImport.WorkspaceRoot is always absolute.
	// Step 4: AutoImport.WorkspaceRootが常に絶対パスであることを保証。
	if r := cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot; r != "" && !filepath.IsAbs(r) {
		abs, err := filepath.Abs(r)
		if err != nil {
			return fmt.Errorf("failed to resolve auto_import.workspace_root %q: %w", r, err)
		}
		cfg.Security.BlockedPaths.AutoImport.WorkspaceRoot = abs
	}

	return nil
}

// applyAllowExecFlags parses and applies --allow-exec flags to the configuration.
// This allows temporary command whitelisting via CLI without modifying the config file.
// The format is "container:command" (e.g., "myapp:npm test").
// Commands containing colons are supported (e.g., "myapp:echo foo:bar").
//
// applyAllowExecFlagsは--allow-execフラグを解析して設定に適用します。
// これにより、設定ファイルを変更せずにCLIから一時的なコマンドホワイトリストが可能です。
// フォーマットは"container:command"です（例: "myapp:npm test"）。
// コロンを含むコマンドもサポートされています（例: "myapp:echo foo:bar"）。
func applyAllowExecFlags(cfg *config.Config, allowExecFlags []string) error {
	// Return early if no flags are provided.
	// フラグが提供されていない場合は早期リターンします。
	if len(allowExecFlags) == 0 {
		return nil
	}

	// Show warning header to indicate temporary whitelist additions.
	// 一時的なホワイトリスト追加を示す警告ヘッダーを表示します。
	slog.Warn("Runtime exec whitelist additions (temporary, will be cleared on restart):")

	// Process each --allow-exec entry.
	// 各--allow-execエントリを処理します。
	for _, entry := range allowExecFlags {
		// Split on first colon only to support commands with colons.
		// コロンを含むコマンドをサポートするため、最初のコロンでのみ分割します。
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid --allow-exec format: %s (expected container:command)", entry)
		}
		container := strings.TrimSpace(parts[0])
		command := strings.TrimSpace(parts[1])

		// Validate container and command are not empty.
		// コンテナとコマンドが空でないことを検証します。
		if container == "" || command == "" {
			return fmt.Errorf("invalid --allow-exec format: %s (container and command cannot be empty)", entry)
		}

		// Add to exec whitelist, creating the map if necessary.
		// execホワイトリストに追加します。必要に応じてマップを作成します。
		if cfg.Security.ExecWhitelist == nil {
			cfg.Security.ExecWhitelist = make(map[string][]string)
		}
		cfg.Security.ExecWhitelist[container] = append(cfg.Security.ExecWhitelist[container], command)

		// Log the added whitelist entry.
		// 追加されたホワイトリストエントリをログに出力します。
		slog.Warn("  Added temporary whitelist",
			"container", container,
			"command", command,
		)
	}

	return nil
}

// defaultDangerousCommands is the list of commands enabled by default in dangerous mode.
// These are common file inspection commands that are useful for debugging.
//
// defaultDangerousCommandsは危険モードでデフォルトで有効になるコマンドのリストです。
// これらはデバッグに便利な一般的なファイル検査コマンドです。
var defaultDangerousCommands = []string{
	"tail",
	"head",
	"cat",
	"grep",
	"less",
	"wc",
	"ls",
	"find",
}

// applyDangerouslyFlags parses and applies --dangerously and --dangerously-all flags.
// These flags enable dangerous mode for exec_command, allowing commands like tail, grep, cat
// with file path validation against blocked_paths.
//
// applyDangerouslyFlagsは--dangerouslyおよび--dangerously-allフラグを解析して適用します。
// これらのフラグはexec_commandの危険モードを有効にし、blocked_pathsに対するファイルパス検証付きで
// tail、grep、catなどのコマンドを許可します。
func applyDangerouslyFlags(cfg *config.Config, dangerously string, dangerouslyAll bool) error {
	// Return early if no flags are set
	// フラグが設定されていない場合は早期リターン
	if dangerously == "" && !dangerouslyAll {
		return nil
	}

	// Cannot use both --dangerously and --dangerously-all together
	// --dangerouslyと--dangerously-allは同時に使用できません
	if dangerously != "" && dangerouslyAll {
		return fmt.Errorf("cannot use both --dangerously and --dangerously-all flags together")
	}

	// Enable dangerous mode
	// 危険モードを有効化
	cfg.Security.ExecDangerously.Enabled = true

	if dangerouslyAll {
		// --dangerously-all: Enable for all containers using "*" key
		// Merge with existing config (if any)
		// --dangerously-all: "*"キーを使用して全コンテナに対して有効化
		// 既存の設定があればマージ
		if cfg.Security.ExecDangerously.Commands == nil {
			cfg.Security.ExecDangerously.Commands = make(map[string][]string)
		}
		cfg.Security.ExecDangerously.Commands["*"] = defaultDangerousCommands

		slog.Warn("Dangerous mode enabled for ALL containers (temporary, will be cleared on restart)",
			"commands", defaultDangerousCommands,
		)
	} else {
		// --dangerously=container1,container2: Enable for specific containers ONLY
		// Clear existing config and only enable for specified containers
		// --dangerously=container1,container2: 指定されたコンテナのみ有効化
		// 既存の設定をクリアし、指定されたコンテナのみ有効化
		cfg.Security.ExecDangerously.Commands = make(map[string][]string)

		containers := strings.Split(dangerously, ",")
		for _, container := range containers {
			container = strings.TrimSpace(container)
			if container == "" {
				continue
			}
			cfg.Security.ExecDangerously.Commands[container] = defaultDangerousCommands
		}

		slog.Warn("Dangerous mode enabled for specific containers ONLY (config file settings cleared)",
			"containers", containers,
			"commands", defaultDangerousCommands,
		)
	}

	return nil
}
