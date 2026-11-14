//go:build !unix

package config

import "os"

func lockDir(string) func() { return func() {} }

func ownerOf(os.FileInfo) (int, int, bool) { return 0, 0, false }
