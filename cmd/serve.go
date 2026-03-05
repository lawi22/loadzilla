package cmd

import (
	"github.com/lawi22/loadzilla/internal/server"
	"github.com/spf13/cobra"
)

var flagPort int

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the web UI",
	RunE: func(cmd *cobra.Command, args []string) error {
		return server.Start(flagPort)
	},
}

func init() {
	serveCmd.Flags().IntVarP(&flagPort, "port", "p", 7777, "Port to listen on")
	rootCmd.AddCommand(serveCmd)
}
