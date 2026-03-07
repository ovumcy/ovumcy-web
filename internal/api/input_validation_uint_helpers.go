package api

import (
	"strconv"
	"strings"
)

func parseRequestUint(raw string) (uint, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(raw), 10, strconv.IntSize)
	if err != nil {
		return 0, err
	}
	return uint(parsed), nil
}
