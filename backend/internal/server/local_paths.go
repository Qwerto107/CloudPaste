package server

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"runtime"
	"strings"
)

// Windows 会把末尾点、空格和备用数据流解释为别名，权限路径必须避免这些歧义。
func validLocalName(name string) bool {
	if name == ".." || strings.ContainsAny(name, "\\\x00") {
		return false
	}
	if runtime.GOOS == "windows" && name != "." {
		return !strings.Contains(name, ":") && !strings.HasSuffix(name, ".") && !strings.HasSuffix(name, " ")
	}
	return true
}

// localDirectory 接管 root，逐层打开真实目录；返回值由调用方关闭。
// 每层只处理一个名称，并比较检查前后的文件身份，避免根内链接和替换竞态。
func localDirectory(root *os.Root, relative string, create bool) (*os.Root, error) {
	for _, name := range strings.Split(relative, "/") {
		if name == "" || name == "." {
			continue
		}
		if !validLocalName(name) {
			_ = root.Close()
			return nil, forbidden()
		}
		before, err := root.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) && create {
			// 单层创建不跟随中间链接；并发创建后仍需重新检查真实对象。
			if err = root.Mkdir(name, 0700); err == nil || errors.Is(err, fs.ErrExist) {
				before, err = root.Lstat(name)
			}
		}
		if err != nil {
			_ = root.Close()
			return nil, localError(err)
		}
		if !before.IsDir() || before.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !os.SameFile(before, before) {
			_ = root.Close()
			return nil, forbidden()
		}
		if err = localCheckName(root, name, before); err != nil {
			_ = root.Close()
			return nil, err
		}
		child, err := root.OpenRoot(name)
		if err != nil {
			_ = root.Close()
			return nil, localError(err)
		}
		opened, openErr := child.Stat(".")
		after, checkErr := root.Lstat(name)
		_ = root.Close()
		if openErr != nil || checkErr != nil || after.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
			_ = child.Close()
			return nil, forbidden()
		}
		root = child
	}
	return root, nil
}

func (d *localDriver) openLocalRoot(create bool) (*os.Root, error) {
	root, err := os.OpenRoot(d.rootPath)
	if err != nil {
		return nil, localError(err)
	}
	return localDirectory(root, d.scope, create)
}

// 将父目录固定为已验证的句柄，后续文件访问只使用单个文件名。
func (d *localDriver) openParent(key string, create bool) (*os.Root, string, error) {
	name := path.Base(localKey(key))
	if !validLocalName(name) {
		return nil, "", forbidden()
	}
	root, err := d.openLocalRoot(create)
	if err != nil {
		return nil, "", err
	}
	root, err = localDirectory(root, path.Dir(localKey(key)), create)
	return root, name, err
}
