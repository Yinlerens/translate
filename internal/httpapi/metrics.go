package httpapi

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Metric names and label names are protocol identifiers; help text is Chinese.
func chineseMetrics(registry *prometheus.Registry) prometheus.Gatherer {
	return prometheus.GathererFunc(func() ([]*dto.MetricFamily, error) {
		families, err := registry.Gather()
		for _, family := range families {
			name := family.GetName()
			help := metricDescriptions[name]
			if help == "" {
				switch {
				case strings.HasPrefix(name, "go_"):
					help = "运行时指标：" + name
				case strings.HasPrefix(name, "process_"):
					help = "进程运行指标：" + name
				default:
					continue
				}
			}
			family.Help = &help
		}
		return families, err
	})
}

var metricDescriptions = map[string]string{
	"go_gc_duration_seconds":           "垃圾回收暂停耗时（秒）",
	"go_goroutines":                    "当前协程数量",
	"go_threads":                       "当前操作系统线程数量",
	"go_info":                          "运行时版本信息",
	"go_memstats_alloc_bytes":          "当前已分配的堆内存（字节）",
	"go_memstats_alloc_bytes_total":    "累计分配的堆内存（字节）",
	"go_memstats_sys_bytes":            "从操作系统获取的内存（字节）",
	"go_memstats_heap_alloc_bytes":     "当前使用的堆内存（字节）",
	"go_memstats_heap_inuse_bytes":     "当前占用的堆内存页（字节）",
	"go_memstats_heap_objects":         "当前堆对象数量",
	"go_memstats_stack_inuse_bytes":    "当前使用的栈内存（字节）",
	"go_memstats_last_gc_time_seconds": "最近一次垃圾回收的时间戳（秒）",
	"process_cpu_seconds_total":        "累计处理器使用时间（秒）",
	"process_resident_memory_bytes":    "进程常驻内存（字节）",
	"process_virtual_memory_bytes":     "进程虚拟内存（字节）",
	"process_start_time_seconds":       "进程启动时间戳（秒）",
	"process_open_fds":                 "当前打开的文件描述符数量",
	"process_max_fds":                  "允许打开的最大文件描述符数量",
}
