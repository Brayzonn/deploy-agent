package logger

import (
	"fmt"
	"os"
	"time"

	"github.com/Brayzonn/deploy-agent/pkg/types"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[0;31m"
	ColorGreen  = "\033[0;32m"
	ColorYellow = "\033[0;33m"
	ColorBlue   = "\033[0;34m"
)

type Logger struct {
	deploymentID string
	logFile      *os.File
}

// ============================================================
// Construction
// ============================================================

/*
New creates a logger for a specific deployment. It ensures logPath
exists and opens a per-deployment log file that every subsequent
call appends to, in addition to always printing to stdout.
*/
func New(deploymentID string, logPath string) (*Logger, error) {

	if err := os.MkdirAll(logPath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	logFileName := fmt.Sprintf("%s/deployment_%s.log", logPath, deploymentID)
	logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	return &Logger{
		deploymentID: deploymentID,
		logFile:      logFile,
	}, nil
}

// ============================================================
// Lifecycle
// ============================================================

/* Close closes the log file. */
func (l *Logger) Close() error {
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}

// ============================================================
// Internal helpers
// ============================================================

/*
log writes a single message with color to stdout and, if a log file
is open, an uncolored copy of the same line to that file.
*/
func (l *Logger) log(color, level, message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	fmt.Printf("%s[%s] [%s] %s%s\n", color, timestamp, level, message, ColorReset)

	if l.logFile != nil {
		logLine := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, message)
		l.logFile.WriteString(logLine)
	}
}

// ============================================================
// Logging
// ============================================================

/* Info logs an informational message. */
func (l *Logger) Info(message string) {
	l.log(ColorBlue, "INFO", message)
}

/* Success logs a success message. */
func (l *Logger) Success(message string) {
	l.log(ColorGreen, "SUCCESS", message)
}

/* Warning logs a warning message. */
func (l *Logger) Warning(message string) {
	l.log(ColorYellow, "WARNING", message)
}

/* Error logs an error message. */
func (l *Logger) Error(message string) {
	l.log(ColorRed, "ERROR", message)
}

/* State logs a deployment state change. */
func (l *Logger) State(state types.DeploymentState) {
	l.Info(fmt.Sprintf("State: %s", state))
}

/*
Fatal logs an error message, closes the log file, and terminates the
process immediately with a non-zero exit code.
*/
func (l *Logger) Fatal(message string) {
	l.Error(message)
	l.Close()
	os.Exit(1)
}

// ============================================================
// Formatted logging
// ============================================================

/* Infof logs a formatted informational message. */
func (l *Logger) Infof(format string, args ...interface{}) {
	l.Info(fmt.Sprintf(format, args...))
}

/* Successf logs a formatted success message. */
func (l *Logger) Successf(format string, args ...interface{}) {
	l.Success(fmt.Sprintf(format, args...))
}

/* Warningf logs a formatted warning message. */
func (l *Logger) Warningf(format string, args ...interface{}) {
	l.Warning(fmt.Sprintf(format, args...))
}

/* Errorf logs a formatted error message. */
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.Error(fmt.Sprintf(format, args...))
}

// ============================================================
// Defaults
// ============================================================

/*
DefaultLogger creates a console-only logger, for use before a
deployment context (and therefore a log file path) exists.
*/
func DefaultLogger() *Logger {
	return &Logger{
		deploymentID: "default",
		logFile:      nil,
	}
}
