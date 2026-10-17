//go:build !windows

package launcher

import "os/exec"

type jobHandle = uintptr

func hideWindow(cmd *exec.Cmd) {}

func attachKillOnCloseJob(cmd *exec.Cmd) (jobHandle, error) { return 0, nil }

func closeJob(job jobHandle) {}
