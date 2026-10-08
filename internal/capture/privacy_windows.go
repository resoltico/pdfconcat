//go:build windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func privateWindowsPath(parent, pattern string) (string, error) {
	prefix, err := privateTempPrefix(pattern)
	if err != nil {
		return "", err
	}

	if parent == "" {
		parent = os.TempDir()
	}

	return filepath.Join(parent, prefix+rand.Text()), nil
}

func createWorkspaceDirectory(parent, pattern string) (string, error) {
	path, err := privateWindowsPath(parent, pattern)
	if err != nil {
		return "", err
	}

	if createErr := createPrivateWindowsDirectory(path); createErr != nil {
		return "", createErr
	}

	return path, nil
}

func createPrivateWindowsDirectory(path string) error {
	attributes, err := privateSecurityAttributes("OICI")
	if err != nil {
		return err
	}

	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode private workspace: %w", err)
	}

	var pinner runtime.Pinner

	pinner.Pin(attributes.SecurityDescriptor)
	defer pinner.Unpin()

	if createErr := windows.CreateDirectory(name, attributes); createErr != nil {
		return fmt.Errorf("create owner-only workspace %q: %w", path, createErr)
	}

	return nil
}

// CreatePrivateTemp atomically creates an owner-only file from an owned terminal-wildcard prefix.
func CreatePrivateTemp(dir, pattern string) (*os.File, error) {
	path, err := privateWindowsPath(dir, pattern)
	if err != nil {
		return nil, err
	}

	return createPrivateWindowsFile(path)
}

func createPrivateWindowsFile(path string) (*os.File, error) {
	attributes, err := privateSecurityAttributes("")
	if err != nil {
		return nil, err
	}

	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode private file: %w", err)
	}

	var pinner runtime.Pinner

	pinner.Pin(attributes.SecurityDescriptor)
	defer pinner.Unpin()

	handle, err := windows.CreateFile(
		name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		attributes, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0,
	)
	if err != nil {
		return nil, fmt.Errorf("create owner-only temporary file: %w", err)
	}

	return os.NewFile(uintptr(handle), path), nil
}

func privateSecurityAttributes(inheritance string) (*windows.SecurityAttributes, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("identify private object owner: %w", err)
	}

	sid := user.User.Sid.String()

	descriptor, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;" + inheritance + ";GA;;;" + sid + ")")
	if err != nil {
		return nil, fmt.Errorf("create private object descriptor: %w", err)
	}

	return &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}, nil
}
