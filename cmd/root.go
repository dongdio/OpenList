// Package cmd implements command-line functionality for OpenList
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	// Import required packages to register their initialization functions
	_ "github.com/dongdio/OpenList/drivers"
	"github.com/dongdio/OpenList/global"
	_ "github.com/dongdio/OpenList/internal/offline_download"
	_ "github.com/dongdio/OpenList/pkg/archive"
)

// Default CLI descriptions
const (
	// ShortDescription is the short description shown in help text
	ShortDescription = "A file list program that supports multiple storage."

	// LongDescription is the long description shown in help text
	LongDescription = `A file list program that supports multiple storage,
built with love by Xhofe and friends in Go/Solid.js.
Complete documentation is available at https://docs.openlist.team/`
)

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "openlist",
	Short: ShortDescription,
	Long:  LongDescription,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// init registers command line flags for the root command
func init() {
	// Define persistent flags that will be available to all subcommands
	RootCmd.PersistentFlags().StringVar(
		&global.DataDir,
		"data",
		"data",
		"Specify the data directory for configuration and storage",
	)

	RootCmd.PersistentFlags().BoolVar(
		&global.Debug,
		"debug",
		false,
		"Enable debug mode with additional logging",
	)

	RootCmd.PersistentFlags().BoolVar(
		&global.NoPrefix,
		"no-prefix",
		false,
		"Disable environment variable prefix (OPENLIST_)",
	)

	RootCmd.PersistentFlags().BoolVar(
		&global.Dev,
		"dev",
		false,
		"Enable development mode with in-memory database",
	)

	RootCmd.PersistentFlags().BoolVar(
		&global.ForceBinDir,
		"force-bin-dir",
		false,
		"Force using the binary location directory as the data directory",
	)

	RootCmd.PersistentFlags().BoolVar(
		&global.LogStd,
		"log-std",
		false,
		"Force logging to standard output instead of file",
	)
}