// version.go implements the 'version' command for displaying the HostMCP version.
// The version is typically set at build time using ldflags.
//
// version.goはHostMCPのバージョンを表示する'version'コマンドを実装します。
// バージョンは通常、ldflagsを使用してビルド時に設定されます。
package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version holds the application version string.
// This is set at build time using ldflags:
// go build -ldflags "-X github.com/YujiSuzuki/hostmcp/internal/cli.Version=1.0.0"
// The default value "dev" indicates a development build.
//
// Versionはアプリケーションのバージョン文字列を保持します。
// これはldflagsを使用してビルド時に設定されます：
// go build -ldflags "-X github.com/YujiSuzuki/hostmcp/internal/cli.Version=1.0.0"
// デフォルト値"dev"は開発ビルドを示します。
var Version = "dev"

// init resolves the version from module build info when not set via ldflags.
// This ensures `go install` users see the correct version (e.g. "v0.2.0")
// instead of the default "dev".
func init() {
	if Version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok &&
			info.Main.Version != "" && info.Main.Version != "(devel)" {
			Version = info.Main.Version
		}
	}
}

// versionCmd represents the 'version' command.
// It prints the version number and exits.
// This is useful for debugging and ensuring the correct version is installed.
//
// versionCmdは'version'コマンドを表します。
// バージョン番号を出力して終了します。
// デバッグや正しいバージョンがインストールされているかの確認に役立ちます。
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of HostMCP",
	Long:  `Print the version number of HostMCP. This version is set at build time.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Simply print the version string to stdout.
		// バージョン文字列を単純にstdoutに出力します。
		fmt.Println(Version)
	},
}

// init registers the version command with the root command.
// This function is automatically called when the package is imported.
//
// initはversionコマンドをルートコマンドに登録します。
// この関数はパッケージがインポートされたときに自動的に呼び出されます。
func init() {
	rootCmd.AddCommand(versionCmd)
}
