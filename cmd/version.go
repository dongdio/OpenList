// Package cmd implements command-line functionality for OpenList
package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/dongdio/OpenList/internal/conf"
)

// Version information template
const versionTemplate = `
Version Information:
  Version:     %s
  Web Version: %s
  Built At:    %s
  Go Version:  %s
  Commit ID:   %s
  Author:      %s
`

// VersionCmd represents the version command
var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show current version of OpenList",
	Long:  "Display detailed version information about the OpenList build",
	Run: func(cmd *cobra.Command, args []string) {
		// Get Go runtime information
		goVersion := fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)

		// Print formatted version information
		fmt.Printf(versionTemplate,
			conf.Version,
			conf.WebVersion,
			conf.BuiltAt,
			goVersion,
			conf.GitCommit,
			conf.GitAuthor,
		)

		os.Exit(0)
	},
}

func init() {
	RootCmd.AddCommand(VersionCmd)
}
