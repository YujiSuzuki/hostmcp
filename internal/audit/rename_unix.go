//go:build !windows

package audit

import (
	"errors"
	"io"
	"os"
	"syscall"
)

// renameOrCopy renames src to dst, falling back to copy+delete when os.Rename
// fails across filesystem boundaries (EXDEV).
//
// renameOrCopyはsrcをdstにリネームします。ファイルシステムをまたぐ場合（EXDEV）は
// コピー＋削除にフォールバックします。
func renameOrCopy(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	// Cross-device fallback: copy then remove source.
	// クロスデバイス対応: コピー後にソースを削除。
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return os.Remove(src)
}
