package local

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
	"github.com/spf13/cobra"
)

// `ufctl auth` command implementation
var AuthCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication tokens",
	Long:  "Commands to manage authentication tokens for accessing Underleaf services.",
}

// `ufctl auth login` prompts the user for their token and saves it securely
var AuthLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login with an authentication token",
	Long:  "Prompts for an authentication token and saves it securely in the local configuration.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		config := config_manager.GetConfig(ctx)

		var token string
		fmt.Print("Enter your authentication token: ")
		fmt.Scanln(&token)

		if err := config.Set("auth.token", token); err != nil {
			return fmt.Errorf("failed to save authentication token: %w", err)
		}

		fmt.Println("Authentication token saved successfully.")
		return nil
	},
}

func init() {
	AuthCmd.AddCommand(AuthLoginCmd)
}
