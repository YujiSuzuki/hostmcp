package cli

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// Verifies that stripHighlightReplaceAttr drops the highlight directive but
// passes every other attribute through unchanged.
//
// stripHighlightReplaceAttrがhighlightという指示だけを取り除き、
// それ以外の属性はそのまま通すことを検証します。
func TestStripHighlightReplaceAttr(t *testing.T) {
	got := stripHighlightReplaceAttr(nil, slog.Bool(highlightAttrKey, true))
	if got.Key != "" {
		t.Errorf("expected highlight attr to be dropped (empty Key), got: %+v", got)
	}

	other := slog.String("name", "docker-compose-up.sh")
	if got := stripHighlightReplaceAttr(nil, other); got.Key != other.Key || !got.Value.Equal(other.Value) {
		t.Errorf("expected non-highlight attr to pass through unchanged, got: %+v", got)
	}
}

// Reproduces the original bug: a record carrying highlight=true, when routed
// through a plain slog.TextHandler (as used for file logging in serve.go)
// without stripHighlightReplaceAttr wired in, printed a literal
// "highlight=true" field with no meaning to a text log consumer. With
// stripHighlightReplaceAttr set as ReplaceAttr, that attribute must not
// appear, while the record's other attributes still do.
//
// 元のバグを再現します: highlight=trueを持つレコードが（serve.goのファイル
// ログで使われるのと同じ）素のslog.TextHandlerに、stripHighlightReplaceAttr
// を組み込まずに渡ると、テキストログの読み手にとって無意味な
// "highlight=true"というフィールドがそのまま出力されていました。
// stripHighlightReplaceAttrをReplaceAttrとして設定すれば、この属性は
// 出力されなくなり、他の属性は引き続き出力されることを確認します。
func TestTextHandlerWithStripHighlightReplaceAttr(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: stripHighlightReplaceAttr,
	})

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "Running host tool", 0)
	r.AddAttrs(slog.String("name", "docker-compose-up.sh"), slog.Bool(highlightAttrKey, true))

	if err := handler.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "highlight") {
		t.Errorf("highlight attribute should be stripped from TextHandler output, got: %q", out)
	}
	if !strings.Contains(out, "name=docker-compose-up.sh") {
		t.Errorf("expected other attributes to still be printed, got: %q", out)
	}
}

// Verifies that a record carrying highlight=true is wrapped in the
// bold+magenta escape codes and that the highlight attribute itself
// is not printed as a regular key=value attribute.
func TestColoredHandlerHighlightsMessage(t *testing.T) {
	var buf bytes.Buffer
	h := &ColoredHandler{out: &buf, level: slog.LevelInfo, colored: true}

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "Running host tool", 0)
	r.AddAttrs(slog.String("name", "docker-compose-up.sh"), slog.Bool("highlight", true))

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	out := buf.String()
	wantHighlighted := styleBold + colorMagenta + "Running host tool" + colorReset
	if !strings.Contains(out, wantHighlighted) {
		t.Errorf("expected message wrapped in bold+magenta, got: %q", out)
	}
	if strings.Contains(out, "highlight=") {
		t.Errorf("highlight attribute should be stripped from output, got: %q", out)
	}
	wantAttr := colorCyan + "name" + colorReset + "=docker-compose-up.sh"
	if !strings.Contains(out, wantAttr) {
		t.Errorf("expected other attributes to still be printed, got: %q", out)
	}
}

// Verifies that a record without the highlight attribute renders the
// message in the default (unstyled) color, as before this feature existed.
func TestColoredHandlerNoHighlightLeavesMessagePlain(t *testing.T) {
	var buf bytes.Buffer
	h := &ColoredHandler{out: &buf, level: slog.LevelInfo, colored: true}

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "Some other log", 0)

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, styleBold) || strings.Contains(out, colorMagenta) {
		t.Errorf("expected no bold/magenta styling without highlight attr, got: %q", out)
	}
}
