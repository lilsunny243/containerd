//go:build windows

/*
   Copyright The containerd Authors.

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

package integration

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/containerd/containerd/integration/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
	v1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type hpcAction func(*testing.T, string, *v1.ContainerConfig)

var (
	defaultCommand                 = WithCommand("Powershell", "/c", "$env:CONTAINER_SANDBOX_MOUNT_POINT/pause.exe")
	localServiceUsername           = WithWindowsUsername("NT AUTHORITY\\Local service")
	localSystemUsername            = WithWindowsUsername("NT AUTHORITY\\System")
	hpcContainerOpt                = WithWindowsHostProcessContainer()
	defaultAction        hpcAction = func(t *testing.T, containerID string, containerConfig *v1.ContainerConfig) {}
)

// Tests to verify the Windows HostProcess
func TestWindowsHostProcess(t *testing.T) {
	pauseImage := images.Get(images.Pause)
	EnsureImageExists(t, pauseImage)

	t.Run("run as Local Service", func(t *testing.T) {
		runHostProcess(t, false, pauseImage, defaultAction, hpcContainerOpt, localServiceUsername, defaultCommand)
	})
	t.Run("run as Local System", func(t *testing.T) {
		runHostProcess(t, false, pauseImage, defaultAction, hpcContainerOpt, localSystemUsername, defaultCommand)
	})
	t.Run("run as unacceptable user", func(t *testing.T) {
		runHostProcess(t, true, pauseImage, defaultAction, hpcContainerOpt, WithWindowsUsername("Guest"), defaultCommand)
	})
	t.Run("run command on host", func(t *testing.T) {
		cmd := WithCommand("Powershell", "/c", "Get-Command containerd.exe")
		runHostProcess(t, false, pauseImage, defaultAction, hpcContainerOpt, localServiceUsername, cmd)
	})
	t.Run("run withHostNetwork", func(t *testing.T) {
		hostname, err := os.Hostname()
		require.NoError(t, err)
		cmd := WithCommand("Powershell", "/c", fmt.Sprintf("if ($env:COMPUTERNAME -ne %s) { exit -1 }", hostname))
		runHostProcess(t, false, pauseImage, defaultAction, hpcContainerOpt, localServiceUsername, cmd)
	})
	t.Run("run with a different os.version image", func(t *testing.T) {
		image := "docker.io/e2eteam/busybox:1.29-windows-amd64-1909"
		EnsureImageExists(t, image)
		runHostProcess(t, false, image, defaultAction, hpcContainerOpt, localServiceUsername, defaultCommand)
	})
	t.Run("run and check stats", func(t *testing.T) {
		var stats = func(t *testing.T, containerID string, containerConfig *v1.ContainerConfig) {
			t.Logf("Fetch stats for container")
			var (
				s   *runtime.ContainerStats
				err error
			)
			require.NoError(t, Eventually(func() (bool, error) {
				s, err = runtimeService.ContainerStats(containerID)
				if err != nil {
					return false, err
				}
				if s.GetWritableLayer().GetUsedBytes().GetValue() != 0 {
					return true, nil
				}
				return false, nil
			}, time.Second, 30*time.Second))

			t.Logf("Verify stats received for container %q", containerConfig)
			testStats(t, s, containerConfig)
		}
		runHostProcess(t, false, pauseImage, stats, hpcContainerOpt, localServiceUsername, defaultCommand, WithTestAnnotations(), WithTestLabels())
	})
}

func runHostProcess(t *testing.T, expectErr bool, image string, action hpcAction, opts ...ContainerOpts) {
	t.Logf("Create a pod config and run sandbox container")
	sb, sbConfig := PodSandboxConfigWithCleanup(t, "sandbox1", "hostprocess", WithWindowsHostProcessPod)

	t.Logf("Create a container config and run container in a pod")
	containerConfig := ContainerConfig(
		"container1",
		image,
		opts...,
	)
	cn, err := runtimeService.CreateContainer(sb, containerConfig, sbConfig)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, runtimeService.RemoveContainer(cn))
	}()
	_, err = t, runtimeService.StartContainer(cn)
	if err != nil {
		if !expectErr {
			t.Fatalf("Unexpected error while starting Container: %v", err)
		}
		return
	}
	defer func() {
		assert.NoError(t, runtimeService.StopContainer(cn, 10))
	}()

	action(t, cn, containerConfig)
}
