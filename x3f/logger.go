package x3f

import (
	"fmt"
	"os"
	"time"
)

// Logger 简洁的进度日志系统
type Logger struct {
	stepStart  time.Time
	totalStart time.Time
}

// NewLogger 创建日志记录器
func NewLogger() *Logger {
	return &Logger{
		totalStart: time.Now(),
	}
}

// Step 开始一个处理步骤
// 格式: [步骤名] 参数 ...
func (l *Logger) Step(name string, params ...interface{}) {
	l.stepStart = time.Now()
	if len(params) > 0 {
		fmt.Printf("[%s] %v ... ", name, params[0])
	} else {
		fmt.Printf("[%s] ", name)
	}
}

// Done 完成当前步骤
// 格式: → 结果 (耗时)
func (l *Logger) Done(result string) {
	elapsed := time.Since(l.stepStart)
	if elapsed > 100*time.Millisecond {
		fmt.Printf("→ %s (%.2fs)\n", result, elapsed.Seconds())
	} else {
		fmt.Printf("→ %s\n", result)
	}
}

// Total 输出总耗时
func (l *Logger) Total() {
	total := time.Since(l.totalStart)
	fmt.Printf("\n✓ 总耗时: %.2fs\n", total.Seconds())
}

// Info 输出信息（不计时）
func (l *Logger) Info(format string, args ...interface{}) {
	fmt.Printf("  • "+format+"\n", args...)
}

// Warn 输出警告
func (l *Logger) Warn(format string, args ...interface{}) {
	fmt.Printf("  ⚠ "+format+"\n", args...)
}

var Debug = debug
var debugEnabled = os.Getenv("DEBUG") != ""

func debug(format string, args ...interface{}) {
	if debugEnabled {
		fmt.Printf(format+"\n", args...)
	}
}
