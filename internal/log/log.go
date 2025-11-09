package log

import (
	"fmt"
	"os"

	"github.com/fatih/color"
)

type LogLevel string

const (
	SCAN   LogLevel = "SCAN"
	FOUND  LogLevel = "FOUND"
	SKIP   LogLevel = "SKIP"
	ACTION LogLevel = "ACTION"
	STOP   LogLevel = "STOP"
	DELETE LogLevel = "DELETE"
	OK     LogLevel = "OK"
	FAIL   LogLevel = "FAIL"
)

var (
	scanColor   = color.New(color.FgCyan)
	foundColor  = color.New(color.FgYellow)
	skipColor   = color.New(color.FgBlue)
	actionColor = color.New(color.FgMagenta)
	stopColor   = color.New(color.FgRed)
	deleteColor = color.New(color.FgRed)
	okColor     = color.New(color.FgGreen)
	failColor   = color.New(color.FgRed)
)

func Log(level LogLevel, message string, args ...interface{}) {
	var c *color.Color
	switch level {
	case SCAN:
		c = scanColor
	case FOUND:
		c = foundColor
	case SKIP:
		c = skipColor
	case ACTION:
		c = actionColor
	case STOP:
		c = stopColor
	case DELETE:
		c = deleteColor
	case OK:
		c = okColor
	case FAIL:
		c = failColor
	default:
		c = color.New()
	}

	formatted := fmt.Sprintf(message, args...)
	fmt.Fprintf(os.Stdout, "%-8s %s\n", c.Sprint(string(level)), formatted)
}
