// client.go implements the 'client' parent command for HTTP-based remote access.
// This command group allows interaction with a HostMCP server from environments
// without direct Docker socket access, such as DevContainers.
//
// client.goはHTTPベースのリモートアクセス用の'client'親コマンドを実装します。
// このコマンドグループにより、DevContainerなどDockerソケットへの直接アクセスがない環境から
// HostMCPサーバーとの対話が可能になります。
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YujiSuzuki/hostmcp/internal/config"
	"github.com/spf13/cobra"
)

var (
	// serverURL holds the HostMCP server URL for client commands.
	// Default is "http://host.docker.internal:18080" which works from within Docker containers.
	// Resolution order: --url flag > HOSTMCP_SERVER_URL env var >
	// server.port from the resolved workspace's hostmcp.yaml (host part stays
	// "host.docker.internal" since server.host is a bind address, not a
	// container-reachable hostname) > this hardcoded default.
	//
	// serverURLはクライアントコマンド用のHostMCPサーバーURLを保持します。
	// デフォルトは"http://host.docker.internal:18080"で、Dockerコンテナ内から動作します。
	// 解決順序: --urlフラグ > HOSTMCP_SERVER_URL環境変数 >
	// 解決されたワークスペースのhostmcp.yamlのserver.port
	// （server.hostはbindアドレスでありコンテナから到達可能なホスト名ではないため、
	// host部分は"host.docker.internal"のまま） > このハードコードされたデフォルト。
	//
	// Deliberately no --workspace flag for this resolution: the $WORKSPACE
	// env var (falling back to "/workspace" when unset) already covers
	// non-default mount paths, such as devcontainers that don't follow the
	// ai-sandbox convention, so an explicit flag would just duplicate that
	// escape hatch. --config remains available for pointing at a specific
	// hostmcp.yaml directly.
	//
	// この解決には意図的に--workspaceフラグを設けていません。$WORKSPACE
	// 環境変数（未設定時は"/workspace"にフォールバック）が、ai-sandbox規約に
	// 従わないdevcontainerなど非デフォルトのマウントパスを既にカバーしており、
	// 明示的なフラグはこの逃げ道と重複するだけです。特定のhostmcp.yamlを
	// 直接指したい場合は--configを使えます。
	serverURL string

	// clientSuffix is appended to the client name for identification.
	// The full client name becomes "hostmcp-go-client_<suffix>".
	// This helps distinguish AI operations from manual user operations.
	// Can be set via --client-suffix flag or HOSTMCP_CLIENT_SUFFIX environment variable.
	// The flag takes precedence over the environment variable.
	//
	// clientSuffixはクライアント名に追加される識別用サフィックスです。
	// 完全なクライアント名は"hostmcp-go-client_<suffix>"になります。
	// これによりAIの操作とユーザーの手動操作を区別できます。
	// --client-suffixフラグまたはHOSTMCP_CLIENT_SUFFIX環境変数で設定できます。
	// フラグが環境変数より優先されます。
	clientSuffix string

	// clientTimeout is the timeout in seconds for HTTP requests and tool call responses.
	// Default is 30 seconds.
	// Can be set via --timeout flag or HOSTMCP_TIMEOUT environment variable.
	// The flag takes precedence over the environment variable.
	//
	// clientTimeoutはHTTPリクエストとツール呼び出しレスポンスのタイムアウト（秒）です。
	// デフォルトは30秒です。
	// --timeoutフラグまたはHOSTMCP_TIMEOUT環境変数で設定できます。
	// フラグが環境変数より優先されます。
	clientTimeout int

	// clientCmd is the cobra.Command struct backing the "client" subcommand.
	// Its Use/Short/Long fields below drive registration and --help output,
	// and its PersistentPreRunE applies the env-var fallbacks for clientSuffix
	// and clientTimeout, plus a three-tier fallback for serverURL (env var,
	// then config-derived server.port, then hardcoded default), before any
	// subcommand's RunE executes.
	//
	// clientCmdは"client"サブコマンドの実体となるcobra.Command構造体です。
	// 以下のUse/Short/Longフィールドが登録内容と--helpの表示内容を決定し、
	// PersistentPreRunEが各サブコマンドのRunE実行前に、clientSuffix/
	// clientTimeoutの環境変数フォールバックと、serverURLの3段階フォールバック
	// （環境変数 → config由来のserver.port → ハードコードされたデフォルト）を
	// 適用します。
	clientCmd = &cobra.Command{
		Use:   "client",
		Short: "Client commands for connecting to HostMCP server via HTTP",
		Long: `Client commands allow you to interact with a running HostMCP server
from environments without direct Docker socket access (like DevContainers).

These commands connect to the HostMCP server via HTTP/MCP API instead of
directly accessing Docker. This is useful when running inside containers
where the Docker socket is not available.`,
		// PersistentPreRunE applies environment variable fallbacks before any subcommand runs.
		// Flag values take precedence over environment variables.
		//
		// PersistentPreRunEはサブコマンド実行前に環境変数のフォールバックを適用します。
		// フラグの値が環境変数より優先されます。
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Fall back to HOSTMCP_SERVER_URL, then to the config-derived
			// server.port, if --url was not explicitly set.
			// --urlが明示的に指定されていない場合、HOSTMCP_SERVER_URL、
			// 次にconfigから解決したserver.portにフォールバックします。
			if !cmd.Flags().Changed("url") {
				if envURL := os.Getenv("HOSTMCP_SERVER_URL"); envURL != "" {
					serverURL = envURL
				} else if port, path, ok := resolveClientServerPort(cfgFile); ok {
					serverURL = fmt.Sprintf("http://host.docker.internal:%d", port)
					// Report the source file so an unexpected port (e.g. from a
					// stray hostmcp.yaml at the fallback workspace path) is
					// immediately traceable instead of silently taking effect.
					// 想定外のポート（フォールバック先のワークスペースにたまたま
					// 存在したhostmcp.yamlなど）が無警告で使われることのないよう、
					// 出所のファイルを表示します。
					fmt.Fprintf(os.Stderr, "HostMCP: using server port %d from %s (override with --url or HOSTMCP_SERVER_URL)\n", port, path)
				}
			}
			// Fall back to HOSTMCP_CLIENT_SUFFIX if --client-suffix was not explicitly set.
			// --client-suffixが明示的に指定されていない場合、HOSTMCP_CLIENT_SUFFIXにフォールバックします。
			if !cmd.Flags().Changed("client-suffix") {
				if envSuffix := os.Getenv("HOSTMCP_CLIENT_SUFFIX"); envSuffix != "" {
					clientSuffix = envSuffix
				}
			}
			// Fall back to HOSTMCP_TIMEOUT if --timeout was not explicitly set.
			// --timeoutが明示的に指定されていない場合、HOSTMCP_TIMEOUTにフォールバックします。
			if !cmd.Flags().Changed("timeout") {
				if envTimeout := os.Getenv("HOSTMCP_TIMEOUT"); envTimeout != "" {
					if v, err := strconv.Atoi(envTimeout); err == nil && v > 0 {
						clientTimeout = v
					}
				}
			}
			return nil
		},
	}
)

// isJapaneseLocale reports whether LC_ALL (falling back to LANG) indicates
// a Japanese locale, matching the same LC_ALL>LANG precedence and "ja_JP"
// prefix check used by writeSponsorMessage in serve.go.
//
// isJapaneseLocaleはLC_ALL（フォールバックでLANG）が日本語ロケールを示すかを
// 返します。serve.goのwriteSponsorMessageと同じLC_ALL>LANGの優先順位、
// "ja_JP"接頭辞判定を使用します。
func isJapaneseLocale() bool {
	lang := os.Getenv("LC_ALL")
	if lang == "" {
		lang = os.Getenv("LANG")
	}
	return strings.HasPrefix(lang, "ja_JP")
}

// init registers the client command and its global flags.
// This function is automatically called when the package is imported.
//
// initはclientコマンドとそのグローバルフラグを登録します。
// この関数はパッケージがインポートされたときに自動的に呼び出されます。
func init() {
	// Add client as a subcommand of the root command.
	// clientをルートコマンドのサブコマンドとして追加します。
	rootCmd.AddCommand(clientCmd)

	// Add --url flag to client command and all subcommands.
	// PersistentFlags are inherited by all subcommands, so list, logs, exec
	// will all have access to the serverURL variable.
	//
	// --urlフラグをclientコマンドとすべてのサブコマンドに追加します。
	// PersistentFlagsはすべてのサブコマンドに継承されるため、list、logs、exec
	// はすべてserverURL変数にアクセスできます。
	urlHelp := "HostMCP server URL (can also be set via HOSTMCP_SERVER_URL environment variable)"
	if isJapaneseLocale() {
		urlHelp = "HostMCPサーバーのURL（HOSTMCP_SERVER_URL環境変数でも設定可能）"
	}
	clientCmd.PersistentFlags().StringVar(&serverURL, "url", "http://host.docker.internal:18080", urlHelp)

	// Add --client-suffix flag to identify the caller.
	// Client name becomes "hostmcp-go-client_<suffix>" when suffix is provided.
	//
	// 呼び出し元を識別するための--client-suffixフラグを追加します。
	// サフィックスが指定されると、クライアント名は"hostmcp-go-client_<suffix>"になります。
	clientSuffixHelp := "Suffix to append to client name (e.g., 'user-cli' becomes 'hostmcp-go-client_user-cli')\n" +
		"Useful when checking logs to distinguish AI operations from manual user operations.\n" +
		"Can also be set via HOSTMCP_CLIENT_SUFFIX environment variable"
	if isJapaneseLocale() {
		clientSuffixHelp = "クライアント名に付加するサフィックス（例: 'user-cli' → 'hostmcp-go-client_user-cli'）\n" +
			"ログを確認する際に、AIによる操作か人による手動操作かを区別するのに役立ちます。\n" +
			"HOSTMCP_CLIENT_SUFFIX環境変数でも設定可能"
	}
	clientCmd.PersistentFlags().StringVarP(&clientSuffix, "client-suffix", "s", "", clientSuffixHelp)

	timeoutHelp := "Timeout in seconds for HTTP requests and tool call responses (default: 30)\n" +
		"Can also be set via HOSTMCP_TIMEOUT environment variable"
	if isJapaneseLocale() {
		timeoutHelp = "HTTPリクエストとツール呼び出しレスポンスのタイムアウト（秒、デフォルト: 30）\n" +
			"HOSTMCP_TIMEOUT環境変数でも設定可能"
	}
	clientCmd.PersistentFlags().IntVar(&clientTimeout, "timeout", 30, timeoutHelp)
}

// resolveClientServerPort attempts to determine server.port from the
// resolved workspace's hostmcp.yaml, for use as a URL fallback default.
// Unlike resolveConfigFile (serve.go), this never errors: if no config file
// can be located, read, or parsed, or the parsed config has no valid
// (positive) server.port, it returns (0, "", false) so the caller
// keeps the hardcoded URL default. This lenient behavior (vs.
// resolveConfigFile's strict, error-returning contract) is intentional:
// client commands must keep working with zero flags and no config file
// present, unlike serve/tools which require an explicit, existing config.
// The resolved path is returned alongside the port purely so the caller can
// tell the user where the port came from (see PersistentPreRunE) — this
// function itself never prints anything.
//
// Workspace resolution order (only used when cfgFile is empty):
//  1. $WORKSPACE env var
//  2. fixed fallback "/workspace" (devcontainer mount convention)
//
// resolveClientServerPortは、解決されたワークスペースのhostmcp.yamlから
// server.portを取得しようとします。resolveConfigFile（serve.go）とは異なり、
// これは決してエラーを返しません。configファイルが見つからない・読めない・
// パースできない場合は(0, "", false)を返し、呼び出し元はハードコードされた
// URLデフォルトを維持します。この寛容な挙動（resolveConfigFileの厳格な
// エラー契約とは対照的）は意図的です。clientコマンドはconfigファイルが
// 存在しない・フラグが一切ない状態でも動作し続ける必要がある一方、
// serve/toolsは明示的に存在するconfigを必須とするためです。
// 解決したパスをポートと一緒に返しているのは、呼び出し元がポートの出所を
// ユーザーに伝えられるようにするためだけです（PersistentPreRunE参照）。
// この関数自体は何も出力しません。
//
// ワークスペース解決順序（cfgFileが空の場合のみ使用）:
//  1. $WORKSPACE環境変数
//  2. 固定のフォールバック"/workspace"（devcontainerマウント規約）
func resolveClientServerPort(cfgFile string) (port int, path string, ok bool) {
	path = cfgFile
	if path == "" {
		ws := os.Getenv("WORKSPACE")
		if ws == "" {
			ws = "/workspace"
		}
		absWs, err := filepath.Abs(ws)
		if err != nil {
			return 0, "", false
		}
		path = filepath.Join(absWs, ".sandbox", "config", "hostmcp.yaml")
	}
	if _, err := os.Stat(path); err != nil {
		return 0, "", false
	}
	cfg, err := config.Load(path)
	if err != nil || cfg.Server.Port <= 0 {
		return 0, "", false
	}
	return cfg.Server.Port, path, true
}
