// Package cli implements the command-line interface for HostMCP.
// It provides commands for starting the MCP server, listing containers,
// viewing logs, executing commands, and client operations for remote access.
//
// cliパッケージはHostMCPのコマンドラインインターフェースを実装します。
// MCPサーバーの起動、コンテナ一覧表示、ログ閲覧、コマンド実行、
// およびリモートアクセス用のクライアント操作のコマンドを提供します。
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// cfgFile holds the path to the configuration file specified via --config.
	// If empty, the serve command requires --workspace to derive the config path;
	// running without either flag returns an error.
	//
	// cfgFileは--configで指定された設定ファイルのパスを保持します。
	// 空の場合、serveコマンドは--workspaceからパスを導出する必要があります。
	// どちらのフラグも指定しない場合はエラーが返されます。
	cfgFile string

	// rootCmd is the base command for the HostMCP CLI.
	// All other commands are added as subcommands to this root command.
	//
	// rootCmdはHostMCP CLIの基本コマンドです。
	// 他のすべてのコマンドはこのルートコマンドのサブコマンドとして追加されます。
	rootCmd = &cobra.Command{
		Use:   "hostmcp",
		Short: "HostMCP - Secure Docker container access for AI assistants",
		Long: `HostMCP provides secure access to Docker containers for AI coding assistants
through MCP (Model Context Protocol). It allows AI tools like Claude Code
and Gemini Code Assist to interact with containers while maintaining
security through whitelisting and permission controls.`,
	}
)

// Execute runs the root command and all its subcommands.
// This is the main entry point for the CLI application.
// It handles command parsing, execution, and error reporting.
// If an error occurs, it prints the error to stderr and exits with code 1.
//
// Executeはルートコマンドとそのすべてのサブコマンドを実行します。
// これはCLIアプリケーションのメインエントリーポイントです。
// コマンドの解析、実行、エラー報告を処理します。
// エラーが発生した場合、stderrにエラーを出力し、終了コード1で終了します。
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// init registers the global flags for the root command.
// This function is automatically called by Go when the package is imported.
//
// initはルートコマンドのグローバルフラグを登録します。
// この関数はパッケージがインポートされたときにGoによって自動的に呼び出されます。
func init() {
	// Register --config flag that can be used with any subcommand.
	// PersistentFlags are inherited by all subcommands.
	//
	// 任意のサブコマンドで使用できる--configフラグを登録します。
	// PersistentFlagsはすべてのサブコマンドに継承されます。
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path (mutually exclusive with --workspace)")
}
