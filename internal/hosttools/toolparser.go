// Package hosttools provides auto-discovery and execution of host-side tools.
// It supports Go (.go), shell (.sh), and Python (.py) files with parsed headers.
//
// hosttoolsパッケージはホスト側ツールの自動検出と実行を提供します。
// Go（.go）、シェル（.sh）、Python（.py）ファイルのヘッダーパースをサポートします。
package hosttools

import (
	"bufio"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ToolInfo holds parsed metadata about a host tool.
// ToolInfoはホストツールの解析済みメタデータを保持します。
type ToolInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Usage       string   `json:"usage,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	Extension   string   `json:"extension"`
	// Timeout is the tool's own declared execution timeout in seconds, parsed
	// from an "@timeout: N" header line. 0 means the tool did not declare one
	// and the caller should fall back to the global default.
	//
	// Timeoutはヘッダー行「@timeout: N」から解析された、ツール自身が宣言する
	// 実行タイムアウト秒数です。0は未宣言を意味し、呼び出し側はグローバル
	// 既定値にフォールバックする必要があります。
	Timeout int `json:"timeout,omitempty"`
}

// maxToolTimeoutSeconds is the largest value that can be multiplied by
// time.Second without overflowing time.Duration (an int64 count of
// nanoseconds). A declared @timeout above this is nonsensical (it would wrap
// around to a garbage duration) and is rejected the same way as a malformed
// value, even though in practice max_tool_timeout clamps effective timeouts
// to something far smaller first.
//
// maxToolTimeoutSecondsは、time.Duration（ナノ秒単位のint64）をオーバーフロー
// させずにtime.Secondと掛け合わせられる最大値です。宣言された@timeoutがこれを
// 超える場合は不正な値（ラップアラウンドしてでたらめな期間になる）として、
// パース失敗時と同様に無視します。実際にはmax_tool_timeoutにより有効な
// タイムアウトはこれよりずっと小さい値にクランプされますが、念のための
// 防御的チェックです。
const maxToolTimeoutSeconds = int(math.MaxInt64 / int64(time.Second))

// parseTimeoutDirective checks whether content is an "@timeout:" header
// directive. ok reports whether the line matched the directive (so callers
// can treat the line as consumed either way); value is 0 when the directive
// was present but its value was malformed, non-positive, or too large to
// convert to time.Duration without overflow — in which case a warning is
// logged with the source file name for diagnosis.
//
// parseTimeoutDirectiveは、contentが「@timeout:」ヘッダーディレクティブかどうかを
// 判定します。okはこの行がディレクティブに一致したか（呼び出し側はいずれにせよ
// この行を消費済み行として扱える）を示し、ディレクティブはあったが値が不正・
// 0以下・time.Duration変換時にオーバーフローする大きさだった場合はvalueを0とし、
// 診断用にファイル名付きで警告ログを出力します。
func parseTimeoutDirective(content, sourceName string) (value int, ok bool) {
	const prefix = "@timeout:"
	if !strings.HasPrefix(content, prefix) {
		return 0, false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(content, prefix))
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 || n > maxToolTimeoutSeconds {
		slog.Warn("Ignoring invalid @timeout directive", "file", sourceName, "value", raw)
		return 0, true
	}
	return n, true
}

// ListTools returns metadata for all tools in the directory,
// filtered by allowed extensions.
//
// ListToolsはディレクトリ内のすべてのツールのメタデータを返します。
// 許可された拡張子でフィルタリングされます。
func ListTools(dir string, allowedExtensions []string) ([]ToolInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %w", err)
	}

	extMap := make(map[string]bool, len(allowedExtensions))
	for _, ext := range allowedExtensions {
		extMap[ext] = true
	}

	var tools []ToolInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if !extMap[ext] {
			continue
		}
		// Skip files starting with underscore (library/helper files)
		// アンダースコアで始まるファイルはスキップ（ライブラリ/ヘルパー）
		if strings.HasPrefix(name, "_") {
			continue
		}

		info, err := parseFileHeader(filepath.Join(dir, name), ext)
		if err != nil {
			continue
		}
		tools = append(tools, info)
	}
	return tools, nil
}

// GetToolInfo returns detailed info for a specific tool by name.
// GetToolInfoは名前で指定されたツールの詳細情報を返します。
func GetToolInfo(dir, name string, allowedExtensions []string) (ToolInfo, error) {
	if err := validateName(name); err != nil {
		return ToolInfo{}, err
	}

	ext := filepath.Ext(name)
	extMap := make(map[string]bool, len(allowedExtensions))
	for _, e := range allowedExtensions {
		extMap[e] = true
	}
	if !extMap[ext] {
		return ToolInfo{}, fmt.Errorf("extension not allowed: %s", ext)
	}

	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		return ToolInfo{}, fmt.Errorf("tool not found: %s", name)
	}

	return parseFileHeader(path, ext)
}

// parseFileHeader extracts metadata from a file's header comments.
// Dispatches to the appropriate parser based on file extension.
//
// parseFileHeaderはファイルのヘッダーコメントからメタデータを抽出します。
// ファイル拡張子に基づいて適切なパーサーにディスパッチします。
func parseFileHeader(path, ext string) (ToolInfo, error) {
	switch ext {
	case ".go":
		return parseGoHeader(path)
	case ".sh":
		return parseShellHeader(path)
	case ".py":
		return parsePythonHeader(path)
	default:
		return ToolInfo{}, fmt.Errorf("unsupported extension: %s", ext)
	}
}

// parseGoHeader extracts metadata from Go file // comments.
// Same format as SandboxMCP toolparser, stops at "// ---" or "package" line.
//
// parseGoHeaderはGoファイルの//コメントからメタデータを抽出します。
// SandboxMCPのtoolparserと同じフォーマットで、"// ---"または"package"行で停止します。
func parseGoHeader(path string) (ToolInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return ToolInfo{}, err
	}
	defer f.Close()

	name := filepath.Base(path)
	info := ToolInfo{Name: name, Extension: ".go"}

	scanner := bufio.NewScanner(f)
	var usageLines []string
	var examples []string
	section := ""

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "package ") {
			break
		}
		if !strings.HasPrefix(line, "//") {
			continue
		}

		content := strings.TrimSpace(strings.TrimPrefix(line, "//"))

		if strings.HasPrefix(content, "---") {
			break
		}

		if v, matched := parseTimeoutDirective(content, name); matched {
			if info.Timeout == 0 {
				info.Timeout = v
			}
			continue
		}

		if info.Description == "" && content != "" {
			info.Description = content
			continue
		}

		if strings.HasPrefix(content, "Usage:") {
			section = "usage"
			continue
		}
		if strings.HasPrefix(content, "Examples:") {
			section = "examples"
			continue
		}

		if content == "" {
			continue
		}

		switch section {
		case "usage":
			usageLines = append(usageLines, content)
		case "examples":
			examples = append(examples, content)
		}
	}

	if len(usageLines) > 0 {
		info.Usage = strings.Join(usageLines, "\n")
	}
	info.Examples = examples

	return info, nil
}

// parseShellHeader extracts metadata from shell script # comments.
// Expected format:
//
//	#!/bin/bash
//	# filename.sh
//	# Description
//
// parseShellHeaderはシェルスクリプトの#コメントからメタデータを抽出します。
func parseShellHeader(path string) (ToolInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return ToolInfo{}, err
	}
	defer f.Close()

	name := filepath.Base(path)
	info := ToolInfo{Name: name, Extension: ".sh"}

	scanner := bufio.NewScanner(f)
	var usageLines []string
	var examples []string
	section := ""
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		if lineNum > 50 {
			break
		}
		line := scanner.Text()

		// Skip shebang
		if lineNum == 1 && strings.HasPrefix(line, "#!") {
			continue
		}

		// Non-comment line ends header
		if !strings.HasPrefix(line, "#") {
			break
		}

		content := strings.TrimSpace(strings.TrimPrefix(line, "#"))

		// Separator stops parsing
		if strings.HasPrefix(content, "---") {
			break
		}

		if v, matched := parseTimeoutDirective(content, name); matched {
			if info.Timeout == 0 {
				info.Timeout = v
			}
			continue
		}

		// First non-empty line after shebang: may be filename, try next
		if info.Description == "" && content != "" {
			// If content looks like a filename (ends with .sh), skip it as name line
			if strings.HasSuffix(content, ".sh") {
				continue
			}
			info.Description = content
			continue
		}

		if strings.HasPrefix(content, "Usage:") || strings.HasPrefix(content, "使用法:") {
			section = "usage"
			continue
		}
		if strings.HasPrefix(content, "Examples:") {
			section = "examples"
			continue
		}

		if content == "" {
			continue
		}

		switch section {
		case "usage":
			usageLines = append(usageLines, content)
		case "examples":
			examples = append(examples, content)
		}
	}

	if len(usageLines) > 0 {
		info.Usage = strings.Join(usageLines, "\n")
	}
	info.Examples = examples

	return info, nil
}

// parsePythonHeader extracts metadata from Python # comments.
// Similar to shell header parsing but also handles docstrings.
//
// Unlike shell/Go headers, there is no Usage:/Examples: section support here
// — only Description (the first non-empty comment line) and an optional
// @timeout: directive are recognized. Scanning continues past the
// Description line (up to the 30-line/non-comment-line limit below) so a
// @timeout: line can be found regardless of whether it appears before or
// after the description line.
//
// parsePythonHeaderはPythonの#コメントからメタデータを抽出します。
//
// shell/Goのヘッダーと異なり、Usage:/Examples:セクションには対応していません
// — Description（最初の非空コメント行）と、任意の@timeout:ディレクティブのみを
// 認識します。@timeout:行がdescription行の前後どちらにあっても見つけられるよう、
// Description確定後もスキャンを継続します（下記の30行制限・非コメント行までは）。
func parsePythonHeader(path string) (ToolInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return ToolInfo{}, err
	}
	defer f.Close()

	name := filepath.Base(path)
	info := ToolInfo{Name: name, Extension: ".py"}

	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		if lineNum > 30 {
			break
		}
		line := scanner.Text()

		// Skip shebang
		if lineNum == 1 && strings.HasPrefix(line, "#!") {
			continue
		}

		// Skip encoding declarations
		if strings.Contains(line, "coding:") || strings.Contains(line, "coding=") {
			continue
		}

		if strings.HasPrefix(line, "#") {
			content := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if content == "" {
				continue
			}
			if v, matched := parseTimeoutDirective(content, name); matched {
				if info.Timeout == 0 {
					info.Timeout = v
				}
				continue
			}
			if info.Description == "" {
				info.Description = content
			}
			continue
		}

		// Non-comment, non-empty: stop
		if strings.TrimSpace(line) != "" {
			break
		}
	}

	return info, nil
}

// validateName checks that a tool name is safe (no path traversal).
// validateNameはツール名が安全であることを確認します（パストラバーサルなし）。
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("empty name")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid name (path traversal): %s", name)
	}
	return nil
}
