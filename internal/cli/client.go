// client.go implements the 'client' parent command for HTTP-based remote access.
// This command group allows interaction with a HostMCP server from environments
// without direct Docker socket access, such as DevContainers.
//
// client.goはHTTPベースのリモートアクセス用の'client'親コマンドを実装します。
// このコマンドグループにより、DevContainerなどDockerソケットへの直接アクセスがない環境から
// HostMCPサーバーとの対話が可能になります。
package cli

import (
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

var (
	// serverURL holds the HostMCP server URL for client commands.
	// Default is "http://host.docker.internal:18080" which works from within Docker containers.
	// Can be overridden via --url flag or HOSTMCP_SERVER_URL environment variable.
	//
	// serverURLはクライアントコマンド用のHostMCPサーバーURLを保持します。
	// デフォルトは"http://host.docker.internal:18080"で、Dockerコンテナ内から動作します。
	// --urlフラグまたはHOSTMCP_SERVER_URL環境変数で上書きできます。
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

	// clientCmd is the parent command for all client subcommands.
	// It groups commands that communicate with the HostMCP server via HTTP/MCP.
	//
	// clientCmdはすべてのクライアントサブコマンドの親コマンドです。
	// HTTP/MCP経由でHostMCPサーバーと通信するコマンドをグループ化します。
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
			// Fall back to HOSTMCP_SERVER_URL if --url was not explicitly set.
			// --urlが明示的に指定されていない場合、HOSTMCP_SERVER_URLにフォールバックします。
			if !cmd.Flags().Changed("url") {
				if envURL := os.Getenv("HOSTMCP_SERVER_URL"); envURL != "" {
					serverURL = envURL
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
	clientCmd.PersistentFlags().StringVar(&serverURL, "url", "http://host.docker.internal:18080",
		"HostMCP server URL (can also be set via HOSTMCP_SERVER_URL environment variable)")

	// Add --client-suffix flag to identify the caller.
	// Client name becomes "hostmcp-go-client_<suffix>" when suffix is provided.
	//
	// 呼び出し元を識別するための--client-suffixフラグを追加します。
	// サフィックスが指定されると、クライアント名は"hostmcp-go-client_<suffix>"になります。
	clientCmd.PersistentFlags().StringVarP(&clientSuffix, "client-suffix", "s", "",
		"Suffix to append to client name (e.g., 'user-cli' becomes 'hostmcp-go-client_user-cli')\n"+
			"Can also be set via HOSTMCP_CLIENT_SUFFIX environment variable")

	// Add --timeout flag to configure request/response timeout in seconds.
	//
	// リクエスト/レスポンスのタイムアウトを秒単位で設定する--timeoutフラグを追加します。
	clientCmd.PersistentFlags().IntVar(&clientTimeout, "timeout", 30,
		"Timeout in seconds for HTTP requests and tool call responses (default: 30)\n"+
			"Can also be set via HOSTMCP_TIMEOUT environment variable")
}
