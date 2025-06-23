// Package cmd implements command-line functionality for OpenList
package cmd

import (
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// RestartCmd represents the command to restart the OpenList server
var RestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the OpenList server",
	Long: `Restart the OpenList server by gracefully stopping the running instance 
and then starting a new instance as a background process.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Log restart operation
		log.Info("Restarting OpenList server...")

		// Stop the current server instance
		stop()

		// Wait a moment to ensure clean shutdown
		time.Sleep(1 * time.Second)

		// Start a new server instance
		start()

		log.Info("Restart operation completed")
	},
}

// init registers the restart command with the root command
func init() {
	RootCmd.AddCommand(RestartCmd)
}
