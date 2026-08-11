package logger

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Logger is a minimal structured key=value logger.
// Safe for concurrent use. Zero allocations when output is io.Discard.
type Logger struct {
	out     io.Writer
	enabled bool
}

// New returns a Logger writing to out.
// Pass io.Discard to silence all output with zero allocation overhead.
func New(out io.Writer) *Logger {
	return &Logger{
		out:     out,
		enabled: out != io.Discard,
	}
}

// Default returns a Logger writing to os.Stdout.
func Default() *Logger {
	return New(os.Stdout)
}

// Info logs a message at INFO level with optional key=value pairs.
func (l *Logger) Info(msg string, kvs ...any) {
	if !l.enabled {
		return
	}
	l.log("INFO", msg, kvs...)
}

// Error logs a message at ERROR level with optional key=value pairs.
func (l *Logger) Error(msg string, kvs ...any) {
	if !l.enabled {
		return
	}
	l.log("ERROR", msg, kvs...)
}

// Debug logs a message at DEBUG level with optional key=value pairs.
func (l *Logger) Debug(msg string, kvs ...any) {
	if !l.enabled {
		return
	}
	l.log("DEBUG", msg, kvs...)
}

func (l *Logger) log(level, msg string, kvs ...any) {
	var b strings.Builder
	b.WriteString("time=")
	b.WriteString(time.Now().UTC().Format(time.RFC3339))
	b.WriteString(" level=")
	b.WriteString(level)
	b.WriteString(" msg=")
	b.WriteString(msg)

	for i := 0; i+1 < len(kvs); i += 2 {
		b.WriteByte(' ')
		b.WriteString(fmt.Sprintf("%v", kvs[i]))
		b.WriteByte('=')
		b.WriteString(fmt.Sprintf("%v", kvs[i+1]))
	}
	b.WriteByte('\n')
	_, _ = io.WriteString(l.out, b.String())
}
