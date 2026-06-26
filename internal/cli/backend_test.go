// backend_test.go contains unit tests for backend.go functions.
//
// backend_test.goはbackend.goの関数の単体テストを含みます。
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/YujiSuzuki/hostmcp/internal/docker"
)

// mockMCPServer is a minimal MCP-over-SSE server used for testing HTTPBackend.
// It handles /health, /sse, and /message endpoints.
// For each "tools/call" request the textResponse callback is invoked; its return value
// becomes Content[0].Text in the SSE response.
//
// mockMCPServerはHTTPBackendのテストに使用するMCP-over-SSEの最小サーバーです。
// /health, /sse, /message エンドポイントを処理します。
// "tools/call"リクエストごとにtextResponseコールバックが呼び出され、その戻り値が
// SSEレスポンスのContent[0].Textになります。
type mockMCPServer struct {
	t            *testing.T
	Server       *httptest.Server
	sseCh        chan string
	mu           sync.Mutex
	LastTool     string
	LastArgs     map[string]interface{}
	textResponse func(toolName string, args map[string]interface{}) string
}

// sseToolResponse is the JSON structure sent back via SSE for a tool call.
// sseToolResponseはツール呼び出しのSSE経由で返すJSON構造体です。
type sseToolResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int            `json:"id"`
	Result  sseToolResult  `json:"result"`
}

type sseToolResult struct {
	Content []sseContent `json:"content"`
}

type sseContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func newMockMCPServer(t *testing.T, textResponse func(toolName string, args map[string]interface{}) string) *mockMCPServer {
	t.Helper()
	m := &mockMCPServer{
		t:            t,
		sseCh:        make(chan string, 10),
		textResponse: textResponse,
	}

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// SSE endpoint: sends endpoint event, then relays messages from sseCh
	// SSEエンドポイント: エンドポイントイベントを送信し、sseCh のメッセージを中継
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not implement http.Flusher")
			return
		}

		// Send session endpoint event so client can extract sessionId
		// クライアントがsessionIdを取得できるようセッションエンドポイントイベントを送信
		fmt.Fprintf(w, "event: endpoint\ndata: /message?sessionId=test-session\n\n")
		flusher.Flush()

		// Relay SSE messages until the request context is cancelled
		// リクエストコンテキストがキャンセルされるまでSSEメッセージを中継
		for {
			select {
			case msg := <-m.sseCh:
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	// Message endpoint: handles initialize and tools/call JSON-RPC requests
	// メッセージエンドポイント: initializeとtools/callのJSON-RPCリクエストを処理
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}

		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		method, _ := req["method"].(string)

		switch method {
		case "initialize":
			// Respond with an MCP initialize result
			// MCP初期化結果で応答
			resp := `{"jsonrpc":"2.0","id":0,"result":{"protocolVersion":"2024-11-05","capabilities":{}}}`
			m.sseCh <- resp
			w.WriteHeader(http.StatusAccepted)

		case "tools/call":
			params, _ := req["params"].(map[string]interface{})
			toolName, _ := params["name"].(string)
			args, _ := params["arguments"].(map[string]interface{})

			m.mu.Lock()
			m.LastTool = toolName
			m.LastArgs = args
			m.mu.Unlock()

			text := m.textResponse(toolName, args)
			resp := sseToolResponse{
				JSONRPC: "2.0",
				ID:      1,
				Result: sseToolResult{
					Content: []sseContent{{Type: "text", Text: text}},
				},
			}
			respBytes, _ := json.Marshal(resp)
			m.sseCh <- string(respBytes)
			w.WriteHeader(http.StatusAccepted)

		default:
			http.Error(w, "unknown method: "+method, http.StatusBadRequest)
		}
	})

	m.Server = httptest.NewServer(mux)
	t.Cleanup(func() { m.Server.Close() })
	return m
}

// newBackend creates an HTTPBackend connected to this mock server.
// newBackendはこのモックサーバーに接続したHTTPBackendを作成します。
func (m *mockMCPServer) newBackend() *HTTPBackend {
	m.t.Helper()
	backend, err := NewHTTPBackendWithSuffix(m.Server.URL, "")
	if err != nil {
		m.t.Fatalf("NewHTTPBackendWithSuffix failed: %v", err)
	}
	m.t.Cleanup(func() { backend.Close() })
	return backend
}

// --- HTTPBackend tests ---

// TestHTTPBackend_ListContainers verifies that ListContainers correctly parses
// the JSON array returned as text content in the MCP response.
//
// TestHTTPBackend_ListContainersはListContainersがMCPレスポンスの
// テキストコンテンツとして返されるJSON配列を正しく解析することを検証します。
func TestHTTPBackend_ListContainers(t *testing.T) {
	wantContainers := []docker.ContainerInfo{
		{ID: "abc123", Name: "test-app", Image: "nginx:latest", State: "running", Status: "Up 2 hours"},
	}

	mock := newMockMCPServer(t, func(toolName string, args map[string]interface{}) string {
		b, _ := json.Marshal(wantContainers)
		return string(b)
	})
	backend := mock.newBackend()

	got, err := backend.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers returned error: %v", err)
	}
	if mock.LastTool != "list_containers" {
		t.Errorf("expected tool 'list_containers', got %q", mock.LastTool)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 container, got %d", len(got))
	}
	if got[0].Name != "test-app" {
		t.Errorf("container Name = %q, want %q", got[0].Name, "test-app")
	}
	if got[0].State != "running" {
		t.Errorf("container State = %q, want %q", got[0].State, "running")
	}
}

// TestHTTPBackend_ListContainers_Empty verifies that an empty content response
// returns an empty slice without error.
//
// TestHTTPBackend_ListContainers_Emptyは空のコンテンツレスポンスが
// エラーなしで空スライスを返すことを検証します。
func TestHTTPBackend_ListContainers_Empty(t *testing.T) {
	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return "[]"
	})
	backend := mock.newBackend()

	got, err := backend.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d containers", len(got))
	}
}

// TestHTTPBackend_GetLogs verifies that GetLogs passes the correct container
// and tail arguments, and returns the raw log text.
//
// TestHTTPBackend_GetLogsはGetLogsが正しいコンテナとtail引数を渡し、
// 生のログテキストを返すことを検証します。
func TestHTTPBackend_GetLogs(t *testing.T) {
	wantLogs := "line1\nline2\nline3\n"

	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return wantLogs
	})
	backend := mock.newBackend()

	got, err := backend.GetLogs(context.Background(), "my-container", "50", "")
	if err != nil {
		t.Fatalf("GetLogs returned error: %v", err)
	}
	if mock.LastTool != "get_logs" {
		t.Errorf("expected tool 'get_logs', got %q", mock.LastTool)
	}
	if mock.LastArgs["container"] != "my-container" {
		t.Errorf("container arg = %v, want %q", mock.LastArgs["container"], "my-container")
	}
	if mock.LastArgs["tail"] != "50" {
		t.Errorf("tail arg = %v, want %q", mock.LastArgs["tail"], "50")
	}
	if _, hasSince := mock.LastArgs["since"]; hasSince {
		t.Error("since arg should be absent when empty string is passed")
	}
	if got != wantLogs {
		t.Errorf("GetLogs = %q, want %q", got, wantLogs)
	}
}

// TestHTTPBackend_GetLogs_WithSince verifies that a non-empty 'since' value
// is included in the tool arguments.
//
// TestHTTPBackend_GetLogs_WithSinceは空でない'since'値が
// ツール引数に含まれることを検証します。
func TestHTTPBackend_GetLogs_WithSince(t *testing.T) {
	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return "filtered logs"
	})
	backend := mock.newBackend()

	_, err := backend.GetLogs(context.Background(), "svc", "100", "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("GetLogs returned error: %v", err)
	}
	if mock.LastArgs["since"] != "2024-01-01T00:00:00Z" {
		t.Errorf("since arg = %v, want %q", mock.LastArgs["since"], "2024-01-01T00:00:00Z")
	}
}

// TestHTTPBackend_Exec verifies that Exec passes dangerously flag and correctly
// extracts the exit code from the response text.
//
// TestHTTPBackend_ExecはExecがdangerouslyフラグを渡し、
// レスポンステキストから終了コードを正しく抽出することを検証します。
func TestHTTPBackend_Exec(t *testing.T) {
	wantOutput := "Command: ls /app\nExit Code: 0\n\nOutput:\nfile1.txt\nfile2.txt"

	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return wantOutput
	})
	backend := mock.newBackend()

	result, err := backend.Exec(context.Background(), "api", "ls /app", false)
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if mock.LastTool != "exec_command" {
		t.Errorf("expected tool 'exec_command', got %q", mock.LastTool)
	}
	if mock.LastArgs["container"] != "api" {
		t.Errorf("container arg = %v, want %q", mock.LastArgs["container"], "api")
	}
	if mock.LastArgs["command"] != "ls /app" {
		t.Errorf("command arg = %v, want %q", mock.LastArgs["command"], "ls /app")
	}
	if mock.LastArgs["dangerously"] != false {
		t.Errorf("dangerously arg = %v, want false", mock.LastArgs["dangerously"])
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Output != wantOutput {
		t.Errorf("Output = %q, want %q", result.Output, wantOutput)
	}
}

// TestHTTPBackend_Exec_NonZeroExit verifies that non-zero exit codes are
// extracted and returned correctly.
//
// TestHTTPBackend_Exec_NonZeroExitはゼロ以外の終了コードが
// 正しく抽出されて返されることを検証します。
func TestHTTPBackend_Exec_NonZeroExit(t *testing.T) {
	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return "Command: cat missing\nExit Code: 1\n\nOutput:\ncat: missing: No such file or directory"
	})
	backend := mock.newBackend()

	result, err := backend.Exec(context.Background(), "api", "cat missing", true)
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
	if mock.LastArgs["dangerously"] != true {
		t.Errorf("dangerously arg = %v, want true", mock.LastArgs["dangerously"])
	}
}

// TestHTTPBackend_GetStats verifies that GetStats returns the raw text response.
//
// TestHTTPBackend_GetStatsはGetStatsが生のテキストレスポンスを返すことを検証します。
func TestHTTPBackend_GetStats(t *testing.T) {
	wantStats := `{"cpu_percent":12.5,"mem_usage":"256MiB"}`

	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return wantStats
	})
	backend := mock.newBackend()

	got, err := backend.GetStats(context.Background(), "db")
	if err != nil {
		t.Fatalf("GetStats returned error: %v", err)
	}
	if mock.LastTool != "get_stats" {
		t.Errorf("expected tool 'get_stats', got %q", mock.LastTool)
	}
	if mock.LastArgs["container"] != "db" {
		t.Errorf("container arg = %v, want %q", mock.LastArgs["container"], "db")
	}
	if got != wantStats {
		t.Errorf("GetStats = %q, want %q", got, wantStats)
	}
}

// TestHTTPBackend_InspectContainer verifies that InspectContainer returns the raw text.
//
// TestHTTPBackend_InspectContainerはInspectContainerが生のテキストを返すことを検証します。
func TestHTTPBackend_InspectContainer(t *testing.T) {
	wantInfo := `{"Id":"abc123","Name":"/test-app"}`

	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return wantInfo
	})
	backend := mock.newBackend()

	got, err := backend.InspectContainer(context.Background(), "test-app")
	if err != nil {
		t.Fatalf("InspectContainer returned error: %v", err)
	}
	if mock.LastTool != "inspect_container" {
		t.Errorf("expected tool 'inspect_container', got %q", mock.LastTool)
	}
	if got != wantInfo {
		t.Errorf("InspectContainer = %q, want %q", got, wantInfo)
	}
}

// TestHTTPBackend_ListHostTools verifies that ListHostTools returns the raw text.
//
// TestHTTPBackend_ListHostToolsはListHostToolsが生のテキストを返すことを検証します。
func TestHTTPBackend_ListHostTools(t *testing.T) {
	wantText := "tool1.sh - does stuff\ntool2.sh - other stuff\n"

	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return wantText
	})
	backend := mock.newBackend()

	got, err := backend.ListHostTools(context.Background())
	if err != nil {
		t.Fatalf("ListHostTools returned error: %v", err)
	}
	if mock.LastTool != "list_host_tools" {
		t.Errorf("expected tool 'list_host_tools', got %q", mock.LastTool)
	}
	if got != wantText {
		t.Errorf("ListHostTools = %q, want %q", got, wantText)
	}
}

// TestHTTPBackend_GetHostToolInfo verifies that the tool name argument is passed.
//
// TestHTTPBackend_GetHostToolInfoはツール名引数が渡されることを検証します。
func TestHTTPBackend_GetHostToolInfo(t *testing.T) {
	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return "Usage: tool1.sh [options]"
	})
	backend := mock.newBackend()

	got, err := backend.GetHostToolInfo(context.Background(), "tool1.sh")
	if err != nil {
		t.Fatalf("GetHostToolInfo returned error: %v", err)
	}
	if mock.LastTool != "get_host_tool_info" {
		t.Errorf("expected tool 'get_host_tool_info', got %q", mock.LastTool)
	}
	if mock.LastArgs["name"] != "tool1.sh" {
		t.Errorf("name arg = %v, want %q", mock.LastArgs["name"], "tool1.sh")
	}
	if got != "Usage: tool1.sh [options]" {
		t.Errorf("GetHostToolInfo = %q, want %q", got, "Usage: tool1.sh [options]")
	}
}

// TestHTTPBackend_RunHostTool verifies that name and args are passed correctly.
//
// TestHTTPBackend_RunHostToolはnameとargsが正しく渡されることを検証します。
func TestHTTPBackend_RunHostTool(t *testing.T) {
	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return "tool output"
	})
	backend := mock.newBackend()

	got, err := backend.RunHostTool(context.Background(), "deploy.sh", []string{"prod", "--dry-run"})
	if err != nil {
		t.Fatalf("RunHostTool returned error: %v", err)
	}
	if mock.LastTool != "run_host_tool" {
		t.Errorf("expected tool 'run_host_tool', got %q", mock.LastTool)
	}
	if mock.LastArgs["name"] != "deploy.sh" {
		t.Errorf("name arg = %v, want %q", mock.LastArgs["name"], "deploy.sh")
	}
	if got != "tool output" {
		t.Errorf("RunHostTool = %q, want %q", got, "tool output")
	}
}

// TestHTTPBackend_ExecHostCommand verifies that command and dangerously are passed.
//
// TestHTTPBackend_ExecHostCommandはcommandとdangerouslyが渡されることを検証します。
func TestHTTPBackend_ExecHostCommand(t *testing.T) {
	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return "host output"
	})
	backend := mock.newBackend()

	got, err := backend.ExecHostCommand(context.Background(), "docker ps", false)
	if err != nil {
		t.Fatalf("ExecHostCommand returned error: %v", err)
	}
	if mock.LastTool != "exec_host_command" {
		t.Errorf("expected tool 'exec_host_command', got %q", mock.LastTool)
	}
	if mock.LastArgs["command"] != "docker ps" {
		t.Errorf("command arg = %v, want %q", mock.LastArgs["command"], "docker ps")
	}
	if mock.LastArgs["dangerously"] != false {
		t.Errorf("dangerously arg = %v, want false", mock.LastArgs["dangerously"])
	}
	if got != "host output" {
		t.Errorf("ExecHostCommand = %q, want %q", got, "host output")
	}
}

// TestHTTPBackend_RestartContainer_WithoutTimeout verifies restart without timeout.
//
// TestHTTPBackend_RestartContainer_WithoutTimeoutはタイムアウトなしの再起動を検証します。
func TestHTTPBackend_RestartContainer_WithoutTimeout(t *testing.T) {
	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return "Container restarted successfully"
	})
	backend := mock.newBackend()

	got, err := backend.RestartContainer(context.Background(), "web", nil)
	if err != nil {
		t.Fatalf("RestartContainer returned error: %v", err)
	}
	if mock.LastTool != "restart_container" {
		t.Errorf("expected tool 'restart_container', got %q", mock.LastTool)
	}
	if mock.LastArgs["container"] != "web" {
		t.Errorf("container arg = %v, want %q", mock.LastArgs["container"], "web")
	}
	if _, hasTimeout := mock.LastArgs["timeout"]; hasTimeout {
		t.Error("timeout arg should be absent when nil is passed")
	}
	if got != "Container restarted successfully" {
		t.Errorf("RestartContainer = %q, want %q", got, "Container restarted successfully")
	}
}

// TestHTTPBackend_RestartContainer_WithTimeout verifies that timeout is included when specified.
//
// TestHTTPBackend_RestartContainer_WithTimeoutはタイムアウトが指定された場合に含まれることを検証します。
func TestHTTPBackend_RestartContainer_WithTimeout(t *testing.T) {
	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return "restarted"
	})
	backend := mock.newBackend()

	timeout := 30
	_, err := backend.RestartContainer(context.Background(), "api", &timeout)
	if err != nil {
		t.Fatalf("RestartContainer returned error: %v", err)
	}
	if mock.LastArgs["timeout"] != float64(30) {
		t.Errorf("timeout arg = %v, want 30", mock.LastArgs["timeout"])
	}
}

// TestHTTPBackend_StopContainer verifies StopContainer with and without timeout.
//
// TestHTTPBackend_StopContainerはタイムアウトありとなしでStopContainerを検証します。
func TestHTTPBackend_StopContainer(t *testing.T) {
	t.Run("without timeout", func(t *testing.T) {
		mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
			return "stopped"
		})
		backend := mock.newBackend()

		_, err := backend.StopContainer(context.Background(), "worker", nil)
		if err != nil {
			t.Fatalf("StopContainer returned error: %v", err)
		}
		if mock.LastTool != "stop_container" {
			t.Errorf("expected tool 'stop_container', got %q", mock.LastTool)
		}
		if _, hasTimeout := mock.LastArgs["timeout"]; hasTimeout {
			t.Error("timeout arg should be absent when nil is passed")
		}
	})

	t.Run("with timeout", func(t *testing.T) {
		mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
			return "stopped"
		})
		backend := mock.newBackend()

		timeout := 10
		_, err := backend.StopContainer(context.Background(), "worker", &timeout)
		if err != nil {
			t.Fatalf("StopContainer returned error: %v", err)
		}
		if mock.LastArgs["timeout"] != float64(10) {
			t.Errorf("timeout arg = %v, want 10", mock.LastArgs["timeout"])
		}
	})
}

// TestHTTPBackend_StartContainer verifies that StartContainer passes the container name.
//
// TestHTTPBackend_StartContainerはStartContainerがコンテナ名を渡すことを検証します。
func TestHTTPBackend_StartContainer(t *testing.T) {
	mock := newMockMCPServer(t, func(_ string, _ map[string]interface{}) string {
		return "started"
	})
	backend := mock.newBackend()

	got, err := backend.StartContainer(context.Background(), "cache")
	if err != nil {
		t.Fatalf("StartContainer returned error: %v", err)
	}
	if mock.LastTool != "start_container" {
		t.Errorf("expected tool 'start_container', got %q", mock.LastTool)
	}
	if mock.LastArgs["container"] != "cache" {
		t.Errorf("container arg = %v, want %q", mock.LastArgs["container"], "cache")
	}
	if got != "started" {
		t.Errorf("StartContainer = %q, want %q", got, "started")
	}
}

// TestHTTPBackend_ServerError verifies that a JSON-RPC error from the server
// is propagated as a Go error.
//
// TestHTTPBackend_ServerErrorはサーバーからのJSON-RPCエラーが
// Goエラーとして伝播されることを検証します。
func TestHTTPBackend_ServerError(t *testing.T) {
	// Send a JSON-RPC error instead of a result
	// 結果の代わりにJSON-RPCエラーを送信
	mux := http.NewServeMux()
	sseCh := make(chan string, 10)

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "event: endpoint\ndata: /message?sessionId=err-session\n\n")
		flusher.Flush()
		for {
			select {
			case msg := <-sseCh:
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		method, _ := req["method"].(string)
		if method == "initialize" {
			sseCh <- `{"jsonrpc":"2.0","id":0,"result":{}}`
			w.WriteHeader(http.StatusAccepted)
			return
		}
		// Send a JSON-RPC error response
		// JSON-RPCエラーレスポンスを送信
		sseCh <- `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"internal error"}}`
		w.WriteHeader(http.StatusAccepted)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close() })

	backend, err := NewHTTPBackendWithSuffix(srv.URL, "")
	if err != nil {
		t.Fatalf("NewHTTPBackendWithSuffix failed: %v", err)
	}
	t.Cleanup(func() { backend.Close() })

	_, err = backend.GetLogs(context.Background(), "app", "10", "")
	if err == nil {
		t.Error("expected error from server, got nil")
	}
}

// TestParseExitCode tests the parseExitCode function.
// It verifies that exit codes are correctly extracted from MCP response text.
//
// TestParseExitCodeはparseExitCode関数をテストします。
// MCPレスポンステキストから終了コードが正しく抽出されることを確認します。
func TestParseExitCode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "standard format exit code 0",
			input:    "Command: pwd\nExit Code: 0\n\nOutput:\n/app",
			expected: 0,
		},
		{
			name:     "standard format exit code 1",
			input:    "Command: cat /nonexistent\nExit Code: 1\n\nOutput:\ncat: /nonexistent: No such file or directory",
			expected: 1,
		},
		{
			name:     "exit code 127 (command not found)",
			input:    "Command: nonexistent\nExit Code: 127\n\nOutput:\nbash: nonexistent: command not found",
			expected: 127,
		},
		{
			name:     "exit code 255",
			input:    "Exit Code: 255\n\nOutput:\nerror",
			expected: 255,
		},
		{
			name:     "no exit code in response",
			input:    "Some output without exit code",
			expected: 0, // Default to success
		},
		{
			name:     "empty response",
			input:    "",
			expected: 0, // Default to success
		},
		{
			name:     "malformed exit code",
			input:    "Exit Code: abc\n\nOutput:\n",
			expected: 0, // Default to success on parse error
		},
		{
			name:     "exit code with extra whitespace",
			input:    "Exit Code:  42 \n\nOutput:\n",
			expected: 0, // Regex expects "Exit Code: N" format exactly
		},
		{
			name:     "multiple exit codes (takes first)",
			input:    "Exit Code: 1\nExit Code: 2\n",
			expected: 1,
		},
		{
			name:     "exit code in middle of text",
			input:    "Some text\nExit Code: 5\nMore text",
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseExitCode(tt.input)
			if got != tt.expected {
				t.Errorf("parseExitCode(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}
