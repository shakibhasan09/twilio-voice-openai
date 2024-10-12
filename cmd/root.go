package cmd

import (
	"fmt"
	"os"

	"github.com/shakibhasan09/twilio-voice-openai/internal"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "twilio-voice-openai",
	Short: "A brief description of your application",
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetInt("port")

		os.Setenv("PORT", fmt.Sprintf("%d", port))

		internal.Run()
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().IntP("port", "p", 1313, "Set the port to listen on")
}
