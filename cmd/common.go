// Package cmd implements command-line functionality for OpenList
package cmd

import (
	"os"
	"path/filepath"
	"strconv"

	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/initialize"
	"github.com/dongdio/OpenList/v4/internal/db"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

const (
	// DaemonDirName is the directory name for daemon-related files
	DaemonDirName = "daemon"

	// PIDFileName is the name of the file storing the process ID
	PIDFileName = "pid"

	// DaemonDirPerm is the permission for the daemon directory
	DaemonDirPerm = 0700
)

// Global daemon-related variables
var (
	// pid stores the process ID from the PID file, -1 if not read yet
	pid = -1

	// pidFilePath stores the full path to the PID file
	pidFilePath string
)

// Init initializes the application
func Init() {
	initialize.InitApp()
}

// Release performs cleanup operations before application shutdown
func Release() {
	db.Close()
}

// initDaemon initializes the daemon mode by reading or creating the PID file
// It sets up the necessary directory structure and reads the existing PID if available
func initDaemon() {
	// Get the executable's directory
	executablePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
		return
	}

	// Create daemon directory
	executableDir := filepath.Dir(executablePath)
	daemonDir := filepath.Join(executableDir, DaemonDirName)

	if err = os.MkdirAll(daemonDir, DaemonDirPerm); err != nil {
		log.Fatalf("Failed to create daemon directory: %v", err)
		return
	}

	// Set PID file path
	pidFilePath = filepath.Join(daemonDir, PIDFileName)

	// Read existing PID if available
	if utils.Exists(pidFilePath) {
		var pidBytes []byte
		pidBytes, err = os.ReadFile(pidFilePath)
		if err != nil {
			log.Fatalf("Failed to read PID file: %v", err)
			return
		}

		pid, err = strconv.Atoi(string(pidBytes))
		if err != nil {
			log.Fatalf("Failed to parse PID data: %v", err)
			return
		}
		log.Debugf("Read existing PID: %d", pid)
	}
}