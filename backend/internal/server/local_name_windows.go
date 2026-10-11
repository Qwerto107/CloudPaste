package server

import (
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Windows 的 Lstat.Name 保留请求的拼写，不能用它识别大小写或 8.3 短名称别名。
// 通过已打开的句柄查询磁盘上的规范长名称，不扫描整个父目录。
func localCheckOpenedName(file *os.File, name string) error {
	if name == "." {
		return nil
	}
	connection, err := file.SyscallConn()
	if err != nil {
		return err
	}
	buffer := make([]uint16, 512)
	var size uint32
	var queryErr error
	const volumeNameNone = 0x4
	err = connection.Control(func(handle uintptr) {
		// FILE_NAME_NORMALIZED | VOLUME_NAME_NONE 只查询规范路径，不依赖盘符映射。
		size, queryErr = windows.GetFinalPathNameByHandle(windows.Handle(handle), &buffer[0], uint32(len(buffer)), volumeNameNone)
		if queryErr == nil && size >= uint32(len(buffer)) && size < 32768 {
			buffer = make([]uint16, size+1)
			size, queryErr = windows.GetFinalPathNameByHandle(windows.Handle(handle), &buffer[0], uint32(len(buffer)), volumeNameNone)
		}
	})
	if err != nil {
		return err
	}
	if queryErr != nil {
		return localError(queryErr)
	}
	if size == 0 || size >= uint32(len(buffer)) || filepath.Base(windows.UTF16ToString(buffer[:size])) != name {
		return forbidden()
	}
	return nil
}

// Stat 和目录遍历也要验证名称；同时确认查询名称的句柄仍指向已检查的对象。
func localCheckName(root *os.Root, name string, expected fs.FileInfo) error {
	if name == "." {
		return nil
	}
	if !os.SameFile(expected, expected) {
		return forbidden()
	}
	file, err := root.Open(name)
	if err != nil {
		return localError(err)
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !os.SameFile(expected, actual) {
		return forbidden()
	}
	return localCheckOpenedName(file, name)
}
