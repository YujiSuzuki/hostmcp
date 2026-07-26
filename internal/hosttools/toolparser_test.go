package hosttools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseGoHeader verifies that Go header comments are parsed correctly,
// including description, usage, and examples sections, and stops at the "---" separator.
//
// TestParseGoHeaderは、Go headerのコメントが正しくパースされることを確認します。
// description、usage、examplesの各セクション、および "---" 区切り文字での停止を検証します。
func TestParseGoHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-tool.go")
	content := `// Short description of the tool
//
// Usage:
//   go run test-tool.go [options]
//
// Examples:
//   go run test-tool.go "hello"
//   go run test-tool.go -v "world"
//
// ---
// Japanese description (not parsed)
package main
`
	os.WriteFile(path, []byte(content), 0644)

	info, err := parseGoHeader(path)
	if err != nil {
		t.Fatalf("parseGoHeader error: %v", err)
	}

	if info.Name != "test-tool.go" {
		t.Errorf("Name = %q, want test-tool.go", info.Name)
	}
	if info.Description != "Short description of the tool" {
		t.Errorf("Description = %q, want 'Short description of the tool'", info.Description)
	}
	if info.Usage != "go run test-tool.go [options]" {
		t.Errorf("Usage = %q, want 'go run test-tool.go [options]'", info.Usage)
	}
	if len(info.Examples) != 2 {
		t.Errorf("Examples length = %d, want 2", len(info.Examples))
	}
	if info.Extension != ".go" {
		t.Errorf("Extension = %q, want .go", info.Extension)
	}
}

// TestParseGoHeader_Timeout verifies that an "@timeout: N" header directive is
// parsed into ToolInfo.Timeout and does not get mistaken for the Description
// line, regardless of where in the header it appears.
//
// TestParseGoHeader_Timeoutは、ヘッダーディレクティブ「@timeout: N」が
// ToolInfo.Timeoutとして解析され、ヘッダー内のどこに書かれてもDescription行と
// 誤認されないことを確認します。
func TestParseGoHeader_Timeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-tool.go")
	content := `// @timeout: 600
// Short description of the tool
package main
`
	os.WriteFile(path, []byte(content), 0644)

	info, err := parseGoHeader(path)
	if err != nil {
		t.Fatalf("parseGoHeader error: %v", err)
	}

	if info.Timeout != 600 {
		t.Errorf("Timeout = %d, want 600", info.Timeout)
	}
	if info.Description != "Short description of the tool" {
		t.Errorf("Description = %q, want 'Short description of the tool'", info.Description)
	}
}

// TestParseShellHeader verifies that shell script header comments are parsed correctly,
// extracting the description and stopping at the "---" separator.
//
// TestParseShellHeaderは、シェルスクリプトのheaderコメントが正しくパースされることを確認します。
// descriptionの抽出と "---" 区切り文字での停止を検証します。
func TestParseShellHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "my-tool.sh")
	content := `#!/bin/bash
# my-tool.sh
# Tool that does something useful
# ---
set -e
echo "hello"
`
	os.WriteFile(path, []byte(content), 0644)

	info, err := parseShellHeader(path)
	if err != nil {
		t.Fatalf("parseShellHeader error: %v", err)
	}

	if info.Name != "my-tool.sh" {
		t.Errorf("Name = %q, want my-tool.sh", info.Name)
	}
	if info.Description != "Tool that does something useful" {
		t.Errorf("Description = %q, want 'Tool that does something useful'", info.Description)
	}
	if info.Extension != ".sh" {
		t.Errorf("Extension = %q, want .sh", info.Extension)
	}
}

// TestParseShellHeader_Timeout verifies that "@timeout: N" is parsed into
// ToolInfo.Timeout and is not mistaken for the Description line, matching
// xcode-test.sh's real header shape (shebang, filename comment, @timeout,
// then description).
//
// TestParseShellHeader_Timeoutは、「@timeout: N」がToolInfo.Timeoutとして
// 解析され、Description行と誤認されないことを確認します（shebang、ファイル名
// コメント、@timeout、descriptionという実際のxcode-test.shのヘッダー構造に
// 合わせています）。
func TestParseShellHeader_Timeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "xcode-test.sh")
	content := `#!/bin/bash
# xcode-test.sh
# @timeout: 600
# Xcode テストをホスト OS上で実行する。
set -e
`
	os.WriteFile(path, []byte(content), 0644)

	info, err := parseShellHeader(path)
	if err != nil {
		t.Fatalf("parseShellHeader error: %v", err)
	}

	if info.Timeout != 600 {
		t.Errorf("Timeout = %d, want 600", info.Timeout)
	}
	if info.Description != "Xcode テストをホスト OS上で実行する。" {
		t.Errorf("Description = %q, want the description line, not the @timeout line", info.Description)
	}
}

// TestParseShellHeader_TimeoutInvalidIgnored verifies that a malformed,
// non-positive, or overflow-prone "@timeout:" value is ignored (Timeout stays
// 0, meaning "use the global default") rather than causing a parse error.
//
// TestParseShellHeader_TimeoutInvalidIgnoredは、不正・0以下・オーバーフローの
// おそれがある「@timeout:」の値が、パースエラーにはならず無視される
// （Timeoutは0のまま=グローバル既定値を使う、を意味する）ことを確認します。
func TestParseShellHeader_TimeoutInvalidIgnored(t *testing.T) {
	cases := []string{
		"# @timeout: not-a-number\n",
		"# @timeout: 0\n",
		"# @timeout: -5\n",
		"# @timeout: 99999999999999999999999999999999\n",
	}
	for _, directive := range cases {
		dir := t.TempDir()
		path := filepath.Join(dir, "tool.sh")
		content := "#!/bin/bash\n# tool.sh\n" + directive + "# A description\n"
		os.WriteFile(path, []byte(content), 0644)

		info, err := parseShellHeader(path)
		if err != nil {
			t.Fatalf("parseShellHeader error for %q: %v", directive, err)
		}
		if info.Timeout != 0 {
			t.Errorf("directive %q: Timeout = %d, want 0 (ignored)", directive, info.Timeout)
		}
		if info.Description != "A description" {
			t.Errorf("directive %q: Description = %q, want 'A description'", directive, info.Description)
		}
	}
}

// TestParsePythonHeader verifies that Python header comments are parsed correctly,
// extracting the description while ignoring shebang and encoding declarations.
//
// TestParsePythonHeaderは、Pythonのheaderコメントが正しくパースされることを確認します。
// shebangやエンコーディング宣言を無視しつつ、descriptionを抽出できることを検証します。
func TestParsePythonHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.py")
	content := `#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Python tool for data processing
import sys
`
	os.WriteFile(path, []byte(content), 0644)

	info, err := parsePythonHeader(path)
	if err != nil {
		t.Fatalf("parsePythonHeader error: %v", err)
	}

	if info.Description != "Python tool for data processing" {
		t.Errorf("Description = %q, want 'Python tool for data processing'", info.Description)
	}
	if info.Extension != ".py" {
		t.Errorf("Extension = %q, want .py", info.Extension)
	}
}

// TestParsePythonHeader_Timeout verifies that "@timeout: N" is parsed into
// ToolInfo.Timeout for Python headers, which — unlike shell/Go — have no
// Usage:/Examples: section support to interact with.
//
// TestParsePythonHeader_Timeoutは、Pythonヘッダーでも「@timeout: N」が
// ToolInfo.Timeoutとして解析されることを確認します。Pythonヘッダーはshell/Goと
// 異なりUsage:/Examples:セクションに対応していないため、それらとの優先順位は
// 考慮不要です。
func TestParsePythonHeader_Timeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.py")
	content := `#!/usr/bin/env python3
# @timeout: 600
# Python tool for data processing
import sys
`
	os.WriteFile(path, []byte(content), 0644)

	info, err := parsePythonHeader(path)
	if err != nil {
		t.Fatalf("parsePythonHeader error: %v", err)
	}

	if info.Timeout != 600 {
		t.Errorf("Timeout = %d, want 600", info.Timeout)
	}
	if info.Description != "Python tool for data processing" {
		t.Errorf("Description = %q, want 'Python tool for data processing'", info.Description)
	}
}

// TestParsePythonHeader_TimeoutAfterDescription is a regression test for the
// parsePythonHeader restructuring: previously the function returned
// immediately after finding the Description line, so scanning had to keep
// going past it in order to find an "@timeout:" line that appears afterward.
//
// TestParsePythonHeader_TimeoutAfterDescriptionは、parsePythonHeaderの構造変更に
// 対する回帰テストです。以前はDescription行を見つけた時点で即座にreturnしていた
// ため、その後に書かれた「@timeout:」行を見つけるにはDescription確定後も
// スキャンを継続する必要があります。
func TestParsePythonHeader_TimeoutAfterDescription(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.py")
	content := `#!/usr/bin/env python3
# Python tool for data processing
# @timeout: 600
import sys
`
	os.WriteFile(path, []byte(content), 0644)

	info, err := parsePythonHeader(path)
	if err != nil {
		t.Fatalf("parsePythonHeader error: %v", err)
	}

	if info.Description != "Python tool for data processing" {
		t.Errorf("Description = %q, want 'Python tool for data processing'", info.Description)
	}
	if info.Timeout != 600 {
		t.Errorf("Timeout = %d, want 600", info.Timeout)
	}
}

// TestListTools_FiltersExtensions verifies that ListTools only includes files
// with allowed extensions and excludes underscore-prefixed files.
//
// TestListTools_FiltersExtensionsは、ListToolsが許可された拡張子のファイルのみを含み、
// アンダースコアで始まるファイルを除外することを確認します。
func TestListTools_FiltersExtensions(t *testing.T) {
	dir := t.TempDir()

	// Create files with different extensions
	files := map[string]string{
		"tool1.go": "// Go tool\npackage main\n",
		"tool2.sh": "#!/bin/bash\n# tool2.sh\n# Shell tool\n",
		"tool3.py": "# Python tool\n",
		"tool4.rb": "# Ruby tool\n",                   // not allowed
		"_lib.go":  "// Library helper\npackage lib\n", // underscore prefix
	}
	for name, content := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
	}

	// Only .go and .sh allowed
	tools, err := ListTools(dir, []string{".go", ".sh"})
	if err != nil {
		t.Fatalf("ListTools error: %v", err)
	}

	if len(tools) != 2 {
		t.Errorf("ListTools returned %d tools, want 2 (tool1.go and tool2.sh)", len(tools))
	}

	// Verify .rb and _lib.go are filtered out
	for _, tool := range tools {
		if tool.Extension == ".rb" {
			t.Error("ListTools should not include .rb files")
		}
		if tool.Name == "_lib.go" {
			t.Error("ListTools should not include underscore-prefixed files")
		}
	}
}

// TestGetToolInfo_PathTraversal verifies that GetToolInfo rejects path traversal
// attempts and paths containing slashes.
//
// TestGetToolInfo_PathTraversalは、GetToolInfoがパストラバーサル攻撃や
// スラッシュを含むパスを拒否することを確認します。
func TestGetToolInfo_PathTraversal(t *testing.T) {
	dir := t.TempDir()

	_, err := GetToolInfo(dir, "../../../etc/passwd", []string{".sh"})
	if err == nil {
		t.Error("GetToolInfo should reject path traversal")
	}

	_, err = GetToolInfo(dir, "sub/tool.go", []string{".go"})
	if err == nil {
		t.Error("GetToolInfo should reject paths with /")
	}
}

// TestGetToolInfo_ExtensionNotAllowed verifies that GetToolInfo rejects files
// with extensions not in the allowed list.
//
// TestGetToolInfo_ExtensionNotAllowedは、GetToolInfoが許可リストに含まれない
// 拡張子のファイルを拒否することを確認します。
func TestGetToolInfo_ExtensionNotAllowed(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tool.rb"), []byte("# Ruby\n"), 0644)

	_, err := GetToolInfo(dir, "tool.rb", []string{".go", ".sh"})
	if err == nil {
		t.Error("GetToolInfo should reject extensions not in allowedExtensions")
	}
}

// TestValidateName verifies that validateName correctly accepts valid filenames
// and rejects empty names, path traversal attempts, and paths with slashes.
//
// TestValidateNameは、validateNameが正しいファイル名を受け入れ、
// 空の名前、パストラバーサル、スラッシュを含むパスを拒否することを確認します。
func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid name", "tool.go", false},
		{"empty name", "", true},
		{"path traversal", "../tool.go", true},
		{"slash in name", "sub/tool.go", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
