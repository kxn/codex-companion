package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
)

type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

var level = Info

func init() {
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		switch strings.ToLower(v) {
		case "debug":
			level = Debug
		case "info":
			level = Info
		case "warn", "warning":
			level = Warn
		case "error":
			level = Error
		}
	}
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func SetLevel(l Level) { level = l }

func logf(l Level, format string, v ...interface{}) {
	if l < level {
		return
	}
	var prefix string
	switch l {
	case Debug:
		prefix = "[DEBUG] "
	case Info:
		prefix = "[INFO] "
	case Warn:
		prefix = "[WARN] "
	case Error:
		prefix = "[ERROR] "
	}
	log.Output(3, prefix+fmt.Sprintf(format, v...))
}

func Debugf(format string, v ...interface{}) { logf(Debug, format, v...) }
func Infof(format string, v ...interface{})  { logf(Info, format, v...) }
func Warnf(format string, v ...interface{})  { logf(Warn, format, v...) }
func Errorf(format string, v ...interface{}) { logf(Error, format, v...) }
