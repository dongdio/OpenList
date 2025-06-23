// Package cmd implements command-line functionality for OpenList
package cmd

import (
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// KillCmd represents the command to forcefully terminate the OpenList server process
var KillCmd = &cobra.Command{
	Use:   "kill",
	Short: "Force kill OpenList server process",
	Long:  "Forcefully terminate the running OpenList server process using the PID from the daemon/pid file",
	Run: func(cmd *cobra.Command, args []string) {
		kill()
	},
}

// kill forcefully terminates the running OpenList process
// It reads the process ID from the PID file, terminates the process,
// and removes the PID file afterward
func kill() {
	// Initialize daemon information and read PID file
	initDaemon()

	// Check if PID is valid
	if pid == -1 {
		log.Info("No running OpenList server detected. Use `openlist start` to start the server.")
		return
	}

	// Find the process by PID
	process, err := os.FindProcess(pid)
	if err != nil {
		log.Errorf("Failed to find process by PID: %d, reason: %v", pid, err)
		return
	}

	// Attempt to kill the process
	if err = process.Kill(); err != nil {
		log.Errorf("Failed to kill process %d: %v", pid, err)
	} else {
		log.Infof("Successfully terminated process: %d", pid)
	}

	// Remove the PID file
	if err = os.Remove(pidFilePath); err != nil {
		log.Errorf("Failed to remove PID file %s: %v", pidFilePath, err)
	} else {
		log.Debugf("Removed PID file: %s", pidFilePath)
	}

	// Reset PID
	pid = -1
}

// init registers the kill command with the root command
func init() {
	RootCmd.AddCommand(KillCmd)
}
