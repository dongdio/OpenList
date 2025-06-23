// Package cmd implements command-line functionality for OpenList
package cmd

import (
	"github.com/spf13/cobra"

	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/pkg/utils"
)

// Cancel2FACmd represents the command to disable two-factor authentication for the admin user
var Cancel2FACmd = &cobra.Command{
	Use:   "cancel2fa",
	Short: "Disable two-factor authentication for admin user",
	Long:  "Remove the two-factor authentication configuration from the admin user's account",
	Run: func(cmd *cobra.Command, args []string) {
		// Initialize the application
		Init()
		defer Release()

		// Get the admin user
		admin, err := op.GetAdmin()
		if err != nil {
			utils.Log.Errorf("Failed to get admin user: %+v", err)
			return
		}

		// Attempt to cancel 2FA for the admin user
		if err := op.Cancel2FAByUser(admin); err != nil {
			utils.Log.Errorf("Failed to disable two-factor authentication: %+v", err)
			return
		}

		// Success - log and clear cache
		utils.Log.Info("Two-factor authentication has been successfully disabled")
		DelAdminCacheOnline()
	},
}

// init registers the cancel2fa command with the root command
func init() {
	RootCmd.AddCommand(Cancel2FACmd)
}
