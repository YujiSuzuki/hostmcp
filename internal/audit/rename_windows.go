//go:build windows

package audit

import "os"

// renameOrCopy renames src to dst. On Windows, os.Rename may fail if src and
// dst are on different drives; cross-drive audit log rotation is not supported.
//
// renameOrCopyはsrcをdstにリネームします。Windowsでは異なるドライブ間のリネームは
// 失敗する場合があります。クロスドライブのローテーションはサポートしていません。
func renameOrCopy(src, dst string) error {
	return os.Rename(src, dst)
}
