// Package audit provides audit logging for HostMCP.
// It records security-relevant events for monitoring and compliance.
//
// auditパッケージはHostMCPの監査ログを提供します。
// セキュリティ関連イベントを監視とコンプライアンスのために記録します。
package audit

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/YujiSuzuki/hostmcp/internal/config"
)

// EventType represents the type of audit event.
// EventTypeは監査イベントのタイプを表します。
type EventType string

const (
	// EventToolCall is logged when an MCP tool is invoked.
	// EventToolCallはMCPツールが呼び出された時にログ記録されます。
	EventToolCall EventType = "tool_call"

	// EventAccessDenied is logged when access is denied.
	// EventAccessDeniedはアクセスが拒否された時にログ記録されます。
	EventAccessDenied EventType = "access_denied"

	// EventClientConnect is logged when a client connects.
	// EventClientConnectはクライアントが接続した時にログ記録されます。
	EventClientConnect EventType = "client_connect"

	// EventClientDisconnect is logged when a client disconnects.
	// EventClientDisconnectはクライアントが切断した時にログ記録されます。
	EventClientDisconnect EventType = "client_disconnect"

	// EventSecurityPolicy is logged when security policy is queried.
	// EventSecurityPolicyはセキュリティポリシーが照会された時にログ記録されます。
	EventSecurityPolicy EventType = "security_policy"
)

// Result represents the outcome of an operation.
// Resultは操作の結果を表します。
type Result string

const (
	ResultSuccess Result = "success"
	ResultDenied  Result = "denied"
	ResultError   Result = "error"
)

// Event represents an audit event.
// Eventは監査イベントを表します。
type Event struct {
	// Type is the type of audit event.
	// Typeは監査イベントのタイプです。
	Type EventType

	// Tool is the name of the MCP tool (for tool_call events).
	// ToolはMCPツールの名前です（tool_callイベント用）。
	Tool string

	// Container is the target container name.
	// Containerは対象コンテナ名です。
	Container string

	// Result is the outcome of the operation.
	// Resultは操作の結果です。
	Result Result

	// ClientName is the name of the MCP client.
	// ClientNameはMCPクライアントの名前です。
	ClientName string

	// SessionID is the unique session identifier.
	// SessionIDはユニークなセッション識別子です。
	SessionID string

	// Details contains additional event-specific information.
	// Detailsは追加のイベント固有情報を含みます。
	Details map[string]any

	// DurationMs is the operation duration in milliseconds.
	// DurationMsは操作の所要時間（ミリ秒）です。
	DurationMs int64

	// ErrorMessage contains the error message if Result is error/denied.
	// ErrorMessageはResultがerror/deniedの場合のエラーメッセージです。
	ErrorMessage string
}

// Logger is the audit logger.
// Loggerは監査ロガーです。
type Logger struct {
	cfg    config.AuditConfig
	logger *slog.Logger
	mu     sync.Mutex
	file   *os.File
	closed bool
}

var (
	globalLogger atomic.Pointer[Logger]
	initMu       sync.Mutex
	initialized  bool
)

// Initialize sets up the global audit logger.
// Idempotent: subsequent calls are no-ops and return nil.
//
// InitializeはグローバルAuditロガーを設定します。
// 冪等: 2回目以降の呼び出しは何もせずnilを返します。
func Initialize(cfg config.AuditConfig) error {
	initMu.Lock()
	defer initMu.Unlock()
	if initialized {
		return nil
	}
	logger, err := newLogger(cfg)
	if err != nil {
		return err
	}
	globalLogger.Store(logger)
	initialized = true
	return nil
}

// expandPath expands "~/" prefix to the user's home directory.
// expandPathは"~/"プレフィックスをユーザーのホームディレクトリに展開します。
func expandPath(path string) (string, error) {
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, path[2:]), nil
}

// rotateFile rotates the log file at path, keeping up to keep old copies.
// Old files are named path.1, path.2, ... path.keep (oldest is deleted).
//
// rotateFileはpathのログファイルをローテーションし、最大keep件の古いコピーを保持します。
// 古いファイルはpath.1, path.2, ... path.keepと命名されます（最も古いものは削除）。
func rotateFile(path string, keep int) error {
	if keep <= 0 {
		return nil
	}
	// Remove oldest file beyond keep limit; fail early if it exists but can't be removed.
	// keep件を超えた最も古いファイルを削除。存在するのに削除できない場合は早期リターン。
	oldest := fmt.Sprintf("%s.%d", path, keep)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove oldest log %s: %w", oldest, err)
	}

	// Shift existing rotated files: .N-1 → .N
	// 既存のローテーションファイルをシフト: .N-1 → .N
	for i := keep - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("failed to stat %s: %w", src, err)
		}
		if err := renameOrCopy(src, dst); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("failed to rotate %s → %s: %w", src, dst, err)
		}
	}

	// Rename current log to .1
	// 現在のログを.1にリネーム
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// nothing to rotate
	} else if err != nil {
		return fmt.Errorf("failed to stat %s: %w", path, err)
	} else if err := renameOrCopy(path, path+".1"); err != nil {
		return fmt.Errorf("failed to rotate %s → %s.1: %w", path, path, err)
	}
	return nil
}

// newLogger creates a new audit logger.
// newLoggerは新しい監査ロガーを作成します。
func newLogger(cfg config.AuditConfig) (*Logger, error) {
	if cfg.File == "" {
		return nil, fmt.Errorf("audit.file must be set when audit logging is enabled")
	}

	l := &Logger{cfg: cfg}

	filePath, err := expandPath(cfg.File)
	if err != nil {
		return nil, err
	}
	cfg.File = filePath
	l.cfg.File = filePath

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create audit log directory: %w", err)
	}

	if err := rotateFile(filePath, cfg.Rotation.Keep); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log %s: %w", filePath, err)
	}
	l.file = f

	l.logger = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	return l, nil
}

// Close closes the audit logger. Safe to call multiple times.
// Closeは監査ロガーをクローズします。複数回呼び出し可能です。
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// FilePath returns the resolved (expanded) path of the audit log file.
//
// FilePathは監査ログファイルの展開済みパスを返します。
func (l *Logger) FilePath() string {
	return l.cfg.File
}

// Log records an audit event.
// Logは監査イベントを記録します。
func (l *Logger) Log(ctx context.Context, event Event) {
	if l == nil || !l.cfg.Enabled {
		return
	}

	// Check if this event type should be logged
	// このイベントタイプをログ記録すべきかチェック
	if !l.shouldLog(event.Type) {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}

	attrs := []any{
		slog.String("event_type", string(event.Type)),
	}

	if event.Tool != "" {
		attrs = append(attrs, slog.String("tool", event.Tool))
	}
	if event.Container != "" {
		attrs = append(attrs, slog.String("container", event.Container))
	}
	if event.Result != "" {
		attrs = append(attrs, slog.String("result", string(event.Result)))
	}
	if event.ClientName != "" {
		attrs = append(attrs, slog.String("client_name", event.ClientName))
	}
	if event.SessionID != "" {
		attrs = append(attrs, slog.String("session_id", event.SessionID))
	}
	if event.DurationMs > 0 {
		attrs = append(attrs, slog.Int64("duration_ms", event.DurationMs))
	}
	if event.ErrorMessage != "" {
		attrs = append(attrs, slog.String("error", event.ErrorMessage))
	}
	if len(event.Details) > 0 {
		attrs = append(attrs, slog.Any("details", event.Details))
	}

	l.logger.InfoContext(ctx, "audit_event", attrs...)
}

// shouldLog checks if an event type should be logged based on config.
// shouldLogは設定に基づいてイベントタイプをログ記録すべきかチェックします。
func (l *Logger) shouldLog(eventType EventType) bool {
	switch eventType {
	case EventToolCall:
		return l.cfg.Events.ToolCalls
	case EventAccessDenied:
		return l.cfg.Events.AccessDenied
	case EventClientConnect, EventClientDisconnect:
		return l.cfg.Events.ClientConnections
	case EventSecurityPolicy:
		return l.cfg.Events.SecurityPolicy
	default:
		return true
	}
}

// LogToolCall logs a tool invocation.
// LogToolCallはツール呼び出しをログ記録します。
func LogToolCall(ctx context.Context, tool, container string, result Result, durationMs int64, details map[string]any) {
	l := globalLogger.Load()
	if l == nil {
		return
	}
	l.Log(ctx, Event{
		Type:       EventToolCall,
		Tool:       tool,
		Container:  container,
		Result:     result,
		DurationMs: durationMs,
		Details:    details,
	})
}

// LogAccessDenied logs an access denial.
// LogAccessDeniedはアクセス拒否をログ記録します。
func LogAccessDenied(ctx context.Context, tool, container, reason string, details map[string]any) {
	l := globalLogger.Load()
	if l == nil {
		return
	}
	l.Log(ctx, Event{
		Type:         EventAccessDenied,
		Tool:         tool,
		Container:    container,
		Result:       ResultDenied,
		ErrorMessage: reason,
		Details:      details,
	})
}

// LogClientConnect logs a client connection.
// LogClientConnectはクライアント接続をログ記録します。
func LogClientConnect(ctx context.Context, clientName, sessionID string) {
	l := globalLogger.Load()
	if l == nil {
		return
	}
	l.Log(ctx, Event{
		Type:       EventClientConnect,
		ClientName: clientName,
		SessionID:  sessionID,
		Result:     ResultSuccess,
	})
}

// LogClientDisconnect logs a client disconnection.
// LogClientDisconnectはクライアント切断をログ記録します。
func LogClientDisconnect(ctx context.Context, clientName, sessionID string, durationMs int64) {
	l := globalLogger.Load()
	if l == nil {
		return
	}
	l.Log(ctx, Event{
		Type:       EventClientDisconnect,
		ClientName: clientName,
		SessionID:  sessionID,
		Result:     ResultSuccess,
		DurationMs: durationMs,
	})
}

// LogSecurityPolicy logs a security policy query.
// LogSecurityPolicyはセキュリティポリシー照会をログ記録します。
func LogSecurityPolicy(ctx context.Context, tool string, details map[string]any) {
	l := globalLogger.Load()
	if l == nil {
		return
	}
	l.Log(ctx, Event{
		Type:    EventSecurityPolicy,
		Tool:    tool,
		Result:  ResultSuccess,
		Details: details,
	})
}

// MeasureDuration is a helper to measure operation duration.
// MeasureDurationは操作の所要時間を計測するヘルパーです。
func MeasureDuration(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}

// GetLogger returns the global audit logger for testing.
// GetLoggerはテスト用にグローバルAuditロガーを返します。
func GetLogger() *Logger {
	return globalLogger.Load()
}

// SetLogger sets the global audit logger (for testing).
// It closes any existing logger and updates the initialized flag accordingly.
//
// SetLoggerはグローバルAuditロガーを設定します（テスト用）。
// 既存ロガーをクローズし、initializedフラグも合わせて更新します。
func SetLogger(l *Logger) {
	initMu.Lock()
	defer initMu.Unlock()
	if existing := globalLogger.Load(); existing != nil {
		existing.Close()
	}
	globalLogger.Store(l)
	initialized = (l != nil)
}

// ResetLogger resets the global audit logger (for testing).
// ResetLoggerはグローバルAuditロガーをリセットします（テスト用）。
func ResetLogger() {
	initMu.Lock()
	defer initMu.Unlock()
	if l := globalLogger.Load(); l != nil {
		l.Close()
	}
	globalLogger.Store(nil)
	initialized = false
}
