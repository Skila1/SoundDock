//go:build !windows

package storage

import "syscall"

func noFollowFlag() int { return syscall.O_NOFOLLOW }
