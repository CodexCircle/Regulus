//go:build !linux

package agent

import "errors"

func diskUsage(string) (total, used uint64, err error) {
	return 0, 0, errors.New("disk usage is only implemented on Linux")
}
