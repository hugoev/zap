package log

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

var (
	// Use colorable output to ensure colors work on all platforms
	colorableOut = colorable.NewColorable(os.Stdout)
)

func init() {
	// Enable colors if stdout is a TTY (terminal)
	// The color library disables colors by default when not a TTY
	if isatty.IsTerminal(os.Stdout.Fd()) {
		color.NoColor = false
	} else {
		// Even if not a TTY, try to enable colors (for CI/CD that supports it)
		color.NoColor = false
	}
}

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
	INFO   LogLevel = "INFO"
	STATS  LogLevel = "STATS"
)

var (
	scanColor   = color.New(color.FgCyan)
	foundColor  = color.New(color.FgYellow)
	skipColor    = color.New(color.FgBlue)
	actionColor = color.New(color.FgMagenta)
	stopColor   = color.New(color.FgRed)
	deleteColor = color.New(color.FgRed)
	okColor     = color.New(color.FgGreen)
	failColor   = color.New(color.FgRed)
	infoColor   = color.New(color.FgCyan) // Changed from white to cyan for better visibility
	statsColor  = color.New(color.FgCyan, color.Bold)
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
	case INFO:
		c = infoColor
	case STATS:
		c = statsColor
	default:
		c = color.New()
	}

	formatted := fmt.Sprintf(message, args...)

	// Use Fprint to write directly to colorable output
	// This ensures colors work properly
	fmt.Fprint(colorableOut, c.Sprint(string(level)))
	fmt.Fprintf(colorableOut, " %s\n", formatted)
}

var Verbose bool = false

func VerboseLog(message string, args ...interface{}) {
	if Verbose {
		Log(INFO, message, args...)
	}
}
