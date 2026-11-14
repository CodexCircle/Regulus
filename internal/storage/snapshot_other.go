//go:build !unix

package storage

import "os"

func copyOwner(*os.File, os.FileInfo) {}

func matchOwner(string) {}
