package cmd

import (
	"fmt"

	"github.com/debugged/deb/buildinfo"
	"github.com/spf13/cobra"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display this binary's version, build time, and git hash of this build",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Version:    %s\n", buildinfo.Version)
		fmt.Printf("Git Hash:   %s\n", buildinfo.GitHash)
		fmt.Printf("Build Time: %s\n", buildinfo.Time)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// versionCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// versionCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
