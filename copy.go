package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func RenameOrCopy(oldpath, newpath string) error {
	err := os.Rename(oldpath, newpath)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	return _copy(oldpath, newpath)
}

func _copy(oldpath, newpath string) error {
	o, err := os.Open(oldpath)
	if err != nil {
		return err
	}
	defer o.Close()
	info, err := o.Stat()
	if err != nil {
		return err
	}

	n, err := os.CreateTemp(filepath.Dir(newpath), ".radicast-copy-*")
	if err != nil {
		return err
	}
	tmpPath := n.Name()
	defer os.Remove(tmpPath)

	_, copyErr := io.Copy(n, o)
	chmodErr := n.Chmod(info.Mode().Perm())
	closeErr := n.Close()
	if err := errors.Join(copyErr, chmodErr, closeErr); err != nil {
		return err
	}
	if err := o.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, newpath); err != nil {
		return err
	}
	return os.Remove(oldpath)
}
