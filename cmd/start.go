// Package cmd implements command-line functionality for OpenList
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	// DefaultLogFileMode defines the permission for log files
	DefaultLogFileMode = 0600

	// DefaultPIDFileMode defines the permission for the PID file
	DefaultPIDFileMode = 0600
)

// StartCmd represents the command to start the OpenList server as a background process
var StartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start OpenList server as a background process",
	Long: `Start the OpenList server as a background daemon process.
This command automatically uses '--force-bin-dir' to ensure the server
uses the binary's directory as the data directory.`,
	Run: func(cmd *cobra.Command, args []string) {
		start()
	},
}

// start launches the OpenList server as a background process
// It checks if the server is already running, creates a new process if not,
// and records the process ID for later management
func start() {
	// Initialize daemon mode and read existing PID if available
	initDaemon()

	// Check if server is already running
	if pid != -1 {
		if _, err := os.FindProcess(pid); err == nil {
			log.Infof("OpenList server is already running with PID %d", pid)
			return
		}
		// Process not found despite PID file, may have been killed abnormally
		log.Warnf("PID file exists but process %d is not running", pid)
	}

	// Prepare command arguments for the server process
	args := os.Args
	args[1] = "server"                     // Replace "start" with "server" command
	args = append(args, "--force-bin-dir") // Force using binary directory

	// Create command to start server in background
	cmd := &exec.Cmd{
		Path: args[0], // Current executable
		Args: args,    // Modified arguments
		Env:  os.Environ(),
	}

	// Configure log file for the server process
	logFilePath := filepath.Join(filepath.Dir(pidFilePath), "start.log")
	logFile, err := os.OpenFile(
		logFilePath,
		os.O_WRONLY|os.O_APPEND|os.O_CREATE,
		DefaultLogFileMode,
	)
	if err != nil {
		log.Fatalf("Failed to open log file %s: %v", logFilePath, err)
	}

	// Redirect stdout and stderr to the log file
	cmd.Stderr = logFile
	cmd.Stdout = logFile

	// Start the server process
	if err = cmd.Start(); err != nil {
		log.Fatalf("Failed to start OpenList server process: %v", err)
	}

	// Log success message
	serverPID := cmd.Process.Pid
	log.Infof("OpenList server started successfully with PID %d", serverPID)

	// Save the PID to the PID file
	if err = os.WriteFile(
		pidFilePath,
		[]byte(strconv.Itoa(serverPID)),
		DefaultPIDFileMode,
	); err != nil {
		log.Warnf("Failed to save PID file: %v", err)
		log.Warn("You may not be able to stop the server using 'openlist stop' command")
	}
}

// init registers the start command with the root command
func init() {
	RootCmd.AddCommand(StartCmd)
}