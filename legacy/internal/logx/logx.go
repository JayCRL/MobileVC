package logx

import (
	"fmt"
	"log"
	"runtime/debug"
	"strings"
)

func Info(component, format string, args ...any) {
	printf("INFO", component, format, args...)
}

func Warn(component, format string, args ...any) {
	printf("WARN", component, format, args...)
}

func Error(component, format string, args ...any) {
	printf("ERROR", component, format, args...)
}

func Recover(component, context string) {
	if recovered := recover(); recovered != nil {
		message := strings.TrimSpace(context)
		if message == "" {
			message = "panic recovered"
		}
		Error(component, "%s: %v\n%s", message, recovered, StackTrace())
	}
}

func StackTrace() string {
	return strings.TrimSpace(string(debug.Stack()))
}

func AuthTokenSummary(token string) string {
	if strings.TrimSpace(token) == "" {
		return "not configured"
	}
	return "configured"
}

func printf(level, component, format string, args ...any) {
	prefix := fmt.Sprintf("[%s]", level)
	if c := strings.TrimSpace(component); c != "" {
		prefix += fmt.Sprintf("[%s]", c)
	}
	if strings.TrimSpace(format) == "" {
		log.Print(prefix)
		return
	}
	log.Printf("%s %s", prefix, fmt.Sprintf(format, args...))
}
