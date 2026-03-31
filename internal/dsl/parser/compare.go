package parser

import (
	"sort"
	"strings"
)

// CompareStrList 比较两个字符串切片是否相等（不考虑顺序）。
// 先对两个切片分别排序，再拼接为字符串进行比较。
// 返回值：0 表示相等，-1 表示 src < dst，1 表示 src > dst。
// 主要用于 DAG 图构建时比较节点的前驱/后继任务列表是否匹配。
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
