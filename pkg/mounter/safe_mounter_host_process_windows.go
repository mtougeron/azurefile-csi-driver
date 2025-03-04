//go:build windows
// +build windows

/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mounter

import (
	"context"
	"fmt"
	"os"
	filepath "path/filepath"
	"strings"

	"k8s.io/klog/v2"
	mount "k8s.io/mount-utils"

	"sigs.k8s.io/azurefile-csi-driver/pkg/os/filesystem"
	"sigs.k8s.io/azurefile-csi-driver/pkg/os/smb"
)

var _ CSIProxyMounter = &winMounter{}

type winMounter struct{}

func NewWinMounter() *winMounter {
	return &winMounter{}
}

func (mounter *winMounter) SMBMount(source, target, fsType string, mountOptions, sensitiveMountOptions []string) error {
	klog.V(2).Infof("SMBMount: remote path: %s local path: %s", source, target)

	if len(mountOptions) == 0 || len(sensitiveMountOptions) == 0 {
		return fmt.Errorf("empty mountOptions(len: %d) or sensitiveMountOptions(len: %d) is not allowed", len(mountOptions), len(sensitiveMountOptions))
	}

	parentDir := filepath.Dir(target)
	parentExists, err := mounter.ExistsPath(parentDir)
	if err != nil {
		return fmt.Errorf("parent dir: %s exist check failed with err: %v", parentDir, err)
	}

	if !parentExists {
		klog.V(2).Infof("Parent directory %s does not exists. Creating the directory", parentDir)
		if err := mounter.MakeDir(parentDir); err != nil {
			return fmt.Errorf("create of parent dir: %s dailed with error: %v", parentDir, err)
		}
	}

	source = strings.Replace(source, "/", "\\", -1)
	normalizedTarget := normalizeWindowsPath(target)

	klog.V(2).Infof("begin to mount %s on %s", source, normalizedTarget)

	remotePath := source
	localPath := normalizedTarget

	if remotePath == "" {
		klog.Errorf("remote path is empty")
		return fmt.Errorf("remote path is empty")
	}

	isMapped, err := smb.IsSmbMapped(remotePath)
	if err != nil {
		isMapped = false
	}

	if isMapped {
		valid, err := filesystem.PathValid(context.Background(), remotePath)
		if err != nil {
			klog.Warningf("PathValid(%s) failed with %v, ignore error", remotePath, err)
		}

		if !valid {
			klog.V(4).Infof("RemotePath %s is not valid, removing now", remotePath)
			err := smb.RemoveSmbGlobalMapping(remotePath)
			if err != nil {
				klog.Errorf("RemoveSmbGlobalMapping(%s) failed with %v", remotePath, err)
				return err
			}
			isMapped = false
		}
	}

	if !isMapped {
		klog.V(4).Infof("Remote %s not mapped. Mapping now!", remotePath)
		username := mountOptions[0]
		password := sensitiveMountOptions[0]
		err := smb.NewSmbGlobalMapping(remotePath, username, password)
		if err != nil {
			klog.Errorf("failed NewSmbGlobalMapping %v", err)
			return err
		}
	}

	if len(localPath) != 0 {
		err = filesystem.ValidatePathWindows(localPath)
		if err != nil {
			klog.Errorf("failed validate plugin path %v", err)
			return err
		}
		err = smb.NewSmbLink(remotePath, localPath)
		if err != nil {
			klog.Errorf("failed NewSmbLink %v", err)
			return fmt.Errorf("creating link %s to %s failed with error: %v", localPath, remotePath, err)
		}
	}

	klog.V(2).Infof("mount %s on %s successfully", source, normalizedTarget)

	return nil
}

func (mounter *winMounter) SMBUnmount(target string) error {
	klog.V(4).Infof("SMBUnmount: local path: %s", target)
	return mounter.Rmdir(target)
}

// Mount just creates a soft link at target pointing to source.
func (mounter *winMounter) Mount(source, target, fstype string, options []string) error {
	return filesystem.LinkPath(normalizeWindowsPath(source), normalizeWindowsPath(target))
}

// Rmdir - delete the given directory
func (mounter *winMounter) Rmdir(path string) error {
	return filesystem.Rmdir(normalizeWindowsPath(path), true)
}

// Unmount - Removes the directory - equivalent to unmount on Linux.
func (mounter *winMounter) Unmount(target string) error {
	klog.V(4).Infof("Unmount: %s", target)
	return mounter.Rmdir(target)
}

func (mounter *winMounter) List() ([]mount.MountPoint, error) {
	return []mount.MountPoint{}, fmt.Errorf("List not implemented for CSIProxyMounter")
}

func (mounter *winMounter) IsMountPoint(file string) (bool, error) {
	isNotMnt, err := mounter.IsLikelyNotMountPoint(file)
	if err != nil {
		return false, err
	}
	return !isNotMnt, nil
}

func (mounter *winMounter) IsMountPointMatch(mp mount.MountPoint, dir string) bool {
	return mp.Path == dir
}

// IsLikelyMountPoint - If the directory does not exists, the function will return os.ErrNotExist error.
// If the path exists, will check if its a link, if its a link then existence of target path is checked.
func (mounter *winMounter) IsLikelyNotMountPoint(path string) (bool, error) {
	isExists, err := mounter.ExistsPath(path)
	if err != nil {
		return false, err
	}
	if !isExists {
		return true, os.ErrNotExist
	}

	response, err := filesystem.IsMountPoint(normalizeWindowsPath(path))
	if err != nil {
		return false, err
	}
	return !response, nil
}

// MakeDir - Creates a directory.
// Currently the make dir is only used from the staging code path, hence we call it
// with Plugin context..
func (mounter *winMounter) MakeDir(path string) error {
	return filesystem.Mkdir(normalizeWindowsPath(path))
}

// ExistsPath - Checks if a path exists. Unlike util ExistsPath, this call does not perform follow link.
func (mounter *winMounter) ExistsPath(path string) (bool, error) {
	return filesystem.PathExists(normalizeWindowsPath(path))
}

func (mounter *winMounter) MountSensitive(source string, target string, fstype string, options []string, sensitiveOptions []string) error {
	return fmt.Errorf("MountSensitive not implemented for winMounter")
}

func (mounter *winMounter) MountSensitiveWithoutSystemd(source string, target string, fstype string, options []string, sensitiveOptions []string) error {
	return fmt.Errorf("MountSensitiveWithoutSystemd not implemented for winMounter")
}

func (mounter *winMounter) MountSensitiveWithoutSystemdWithMountFlags(source string, target string, fstype string, options []string, sensitiveOptions []string, mountFlags []string) error {
	return mounter.MountSensitive(source, target, fstype, options, sensitiveOptions /* sensitiveOptions */)
}

func (mounter *winMounter) GetMountRefs(pathname string) ([]string, error) {
	return []string{}, fmt.Errorf("GetMountRefs not implemented for winMounter")
}

func (mounter *winMounter) EvalHostSymlinks(pathname string) (string, error) {
	return "", fmt.Errorf("EvalHostSymlinks not implemented for winMounter")
}

func (mounter *winMounter) GetFSGroup(pathname string) (int64, error) {
	return -1, fmt.Errorf("GetFSGroup not implemented for winMounter")
}

func (mounter *winMounter) GetSELinuxSupport(pathname string) (bool, error) {
	return false, fmt.Errorf("GetSELinuxSupport not implemented for winMounter")
}

func (mounter *winMounter) GetMode(pathname string) (os.FileMode, error) {
	return 0, fmt.Errorf("GetMode not implemented for winMounter")
}

// GetAPIVersions returns the versions of the client APIs this mounter is using.
func (mounter *winMounter) GetAPIVersions() string {
	return ""
}

func (mounter *winMounter) CanSafelySkipMountPointCheck() bool {
	return false
}
