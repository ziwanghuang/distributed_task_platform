package parser

import (
	"sort"
	"strings"
)

func CompareStrList(src, dst []string) int {
	sort.Slice(src, func(i, j int) bool {
		return src[i] < src[j]
	})
	sort.Slice(dst, func(i, j int) bool {
		return dst[i] < dst[j]
	})
	srcStr := strings.Join(src, ":")
	dstStr := strings.Join(dst, ":")
	switch {
	case srcStr == dstStr:
		return 0
	case srcStr < dstStr:
		return -1
	default:
		return 1
	}
}
