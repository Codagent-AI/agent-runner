package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "agent-runner",
		Short: "CLI workflow orchestrator for AI agents",
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
		os.Exit(1)
	}
}
