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


func New(deploymentID string, logPath string) (*Logger, error) {
	
	if err := os.MkdirAll(logPath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Create log file
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

//  close the log file
func (l *Logger) Close() error {
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}

//  write a message with color to both stdout and log file
func (l *Logger) log(color, level, message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	
	fmt.Printf("%s[%s] [%s] %s%s\n", color, timestamp, level, message, ColorReset)
	
	if l.logFile != nil {
		logLine := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, message)
		l.logFile.WriteString(logLine)
	}
}

//  log an informational message
func (l *Logger) Info(message string) {
	l.log(ColorBlue, "INFO", message)
}

//  log a success message
func (l *Logger) Success(message string) {
	l.log(ColorGreen, "SUCCESS", message)
}

//  log a warning message
func (l *Logger) Warning(message string) {
	l.log(ColorYellow, "WARNING", message)
}

//  log an error message
func (l *Logger) Error(message string) {
	l.log(ColorRed, "ERROR", message)
}

//  log a state change
func (l *Logger) State(state types.DeploymentState) {
	l.Info(fmt.Sprintf("State: %s", state))
}

//  log an error and exits
func (l *Logger) Fatal(message string) {
	l.Error(message)
	l.Close()
	os.Exit(1)
}

//  log a formatted informational message
func (l *Logger) Infof(format string, args ...interface{}) {
	l.Info(fmt.Sprintf(format, args...))
}

//  log a formatted success message
func (l *Logger) Successf(format string, args ...interface{}) {
	l.Success(fmt.Sprintf(format, args...))
}

//  log a formatted warning message
func (l *Logger) Warningf(format string, args ...interface{}) {
	l.Warning(fmt.Sprintf(format, args...))
}

//  log a formatted error message
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.Error(fmt.Sprintf(format, args...))
}

//  create a basic logger that only outputs to console
func DefaultLogger() *Logger {
	return &Logger{
		deploymentID: "default",
		logFile:      nil,
	}
}