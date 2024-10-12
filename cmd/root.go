package cmd

import (
	"os"

	"github.com/shakibhasan09/twilio-voice-openai/internal"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "twilio-voice-openai",
	Short: "An AI-powered voice assistant using Twilio and OpenAI",
	Run: func(cmd *cobra.Command, args []string) {
		internal.Run()
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {}
