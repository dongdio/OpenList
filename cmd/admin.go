// Package cmd implements command-line functionality for OpenList
package cmd

import (
	"github.com/spf13/cobra"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/utility/utils"
	"github.com/dongdio/OpenList/v4/utility/utils/random"
)

// Constants for admin-related functionality
const (
	// DefaultRandomPasswordLength defines the length of auto-generated passwords
	DefaultRandomPasswordLength = 12

	// AdminInfoMessage is displayed when showing admin information
	AdminInfoMessage = `Admin Information:
- Username: %s
- Password is stored as a hash value and cannot be reversed
- Reset password with: openlist admin random
- Set new password with: openlist admin set NEW_PASSWORD
`
)

// AdminCmd represents the admin command for managing administrator accounts
var AdminCmd = &cobra.Command{
	Use:     "admin",
	Aliases: []string{"password"},
	Short:   "Show and manage admin user information",
	Long:    "Display admin user information and perform operations related to the admin password",
	Run: func(cmd *cobra.Command, args []string) {
		Init()
		defer Release()

		admin, err := op.GetAdmin()
		if err != nil {
			utils.Log.Errorf("Failed to get admin user: %+v", err)
			return
		}

		utils.Log.Infof(AdminInfoMessage, admin.Username)
	},
}

// RandomPasswordCmd generates a random password for the admin user
var RandomPasswordCmd = &cobra.Command{
	Use:   "random",
	Short: "Reset admin user's password to a random string",
	Long:  "Generate a secure random password and set it for the admin user",
	Run: func(cmd *cobra.Command, args []string) {
		// Generate a random password with improved length for better security
		newPassword := random.String(DefaultRandomPasswordLength)
		setAdminPassword(newPassword)
	},
}

// SetPasswordCmd sets a specific password for the admin user
var SetPasswordCmd = &cobra.Command{
	Use:   "set NEW_PASSWORD",
	Short: "Set admin user's password",
	Long:  "Set a specific password for the admin user",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		setAdminPassword(args[0])
	},
}

// ShowTokenCmd displays the admin authentication token
var ShowTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Show admin token",
	Long:  "Display the authentication token used for admin API access",
	Run: func(cmd *cobra.Command, args []string) {
		Init()
		defer Release()

		token := setting.GetStr(consts.Token)
		utils.Log.Infof("Admin token: %s", token)
	},
}

// setAdminPassword updates the admin user's password
// It initializes the application, updates the password, and clears the admin cache
func setAdminPassword(password string) {
	Init()
	defer Release()

	// Get the admin user
	admin, err := op.GetAdmin()
	if err != nil {
		utils.Log.Errorf("Failed to get admin user: %+v", err)
		return
	}

	// Set the new password
	admin.SetPassword(password)

	// Update the user in the database
	if err = op.UpdateUser(admin); err != nil {
		utils.Log.Errorf("Failed to update admin user: %+v", err)
		return
	}

	// Log success information
	utils.Log.Infof("Admin user has been updated successfully:")
	utils.Log.Infof("Username: %s", admin.Username)
	utils.Log.Infof("Password: %s", password)

	// Clear the admin cache to ensure the new password takes effect immediately
	DelAdminCacheOnline()
}

// init registers the admin commands with the root command
func init() {
	// Add admin command to root command
	RootCmd.AddCommand(AdminCmd)

	// Add subcommands to admin command
	AdminCmd.AddCommand(RandomPasswordCmd)
	AdminCmd.AddCommand(SetPasswordCmd)
	AdminCmd.AddCommand(ShowTokenCmd)
}