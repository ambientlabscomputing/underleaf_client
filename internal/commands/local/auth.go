package local

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_client/internal/auth"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	internalutils "github.com/ambientlabscomputing/underleaf_client/internal/utils"
)

var (
	noBrowser bool
	timeout   time.Duration
)

// Styles for the auth flow display
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginBottom(1)

	codeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FF00")).
			Background(lipgloss.Color("#1a1a1a")).
			Padding(0, 1)

	urlStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00BFFF")).
			Underline(true)

	instructionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#999999")).
				MarginTop(1)

	successStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FF00"))

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF0000"))
)

// `ufctl auth` command implementation
var AuthCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
	Long:  "Commands to manage authentication for accessing Underleaf services.",
}

// `ufctl auth login` uses OAuth2 device code flow for authentication
var AuthLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login using device authorization",
	Long:  "Authenticate using OAuth2 device code flow. Opens a browser for authorization.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)

		// Get dependencies
		deps := utils.NewDependencyManager(ctx)
		if deps == nil {
			return fmt.Errorf("failed to create dependencies")
		}

		config := deps.ConfigClient
		authClient := deps.CPlaneClient.Auth

		// Start the device authorization flow
		fmt.Println(titleStyle.Render("🔐 Underleaf Authentication"))
		fmt.Println()
		fmt.Println("Initiating device authorization flow...")
		logger.Info("Starting device authorization flow")

		authResp, err := authClient.StartDeviceFlow(ctx)
		if err != nil {
			return fmt.Errorf("failed to start device flow: %w", err)
		}
		logger.Info("Device flow started", "device_code", authResp.DeviceCode, "user_code", authResp.UserCode, "expires_in", authResp.ExpiresIn)

		// Determine which URL to use (prefer complete URL with code pre-filled)
		displayURL := authResp.VerificationURIComplete
		if displayURL == "" {
			displayURL = authResp.VerificationURI
		}

		// Display the URL and code
		fmt.Println()
		fmt.Println("Please authorize this device by visiting:")
		fmt.Println()
		fmt.Println("  " + urlStyle.Render(displayURL))
		fmt.Println()

		// Only show the code if we're using the basic verification URI
		if authResp.VerificationURIComplete == "" {
			fmt.Println("And enter the code:")
			fmt.Println()
			fmt.Println("  " + codeStyle.Render(authResp.UserCode))
			fmt.Println()
		}

		// Open browser unless --no-browser flag is set
		if !noBrowser {
			if err := internalutils.OpenURL(displayURL); err != nil {
				fmt.Println(instructionStyle.Render(fmt.Sprintf("⚠️  Could not open browser: %v", err)))
				fmt.Println(instructionStyle.Render("Please open the URL manually."))
			} else {
				fmt.Println(instructionStyle.Render("✓ Browser opened automatically"))
			}
		}

		fmt.Println()
		fmt.Println(instructionStyle.Render(fmt.Sprintf("Code expires in %d seconds", authResp.ExpiresIn)))
		fmt.Println(instructionStyle.Render("Waiting for authorization..."))
		fmt.Println()

		// Create a spinner model for polling
		spinner := NewAuthSpinner()
		program := tea.NewProgram(spinner)

		// Start polling in the background
		pollerConfig := auth.DefaultPollerConfig()
		if timeout > 0 {
			pollerConfig.Timeout = timeout
		}
		logger.Info("Starting poller", "initial_interval", pollerConfig.InitialInterval, "timeout", pollerConfig.Timeout)
		poller := auth.NewPoller(authClient, pollerConfig)
		resultChan := poller.Poll(ctx, authResp.DeviceCode)
		logger.Info("Poller started, waiting for result")

		// Run the spinner and wait for result
		go func() {
			logger.Info("Result channel goroutine started")
			for result := range resultChan {
				logger.Info("Received result from poller", "has_error", result.Error != nil, "has_token", result.Token != nil)
				if result.Error != nil {
					logger.Error("Auth error received", "error", result.Error)
					program.Send(authErrorMsg{err: result.Error})
				} else {
					logger.Info("Auth success received", "access_token_length", len(result.Token.AccessToken))
					program.Send(authSuccessMsg{token: result.Token})
				}
			}
			logger.Info("Result channel closed")
		}()

		logger.Info("Starting Bubble Tea program")
		finalModel, err := program.Run()
		logger.Info("Bubble Tea program finished")
		if err != nil {
			return fmt.Errorf("error running UI: %w", err)
		}

		// Check the final state
		logger.Info("Checking final state")
		final := finalModel.(authSpinnerModel)
		if final.err != nil {
			logger.Error("Final state has error", "error", final.err)
			return final.err
		}

		if final.token == nil {
			logger.Error("Final state has no token")
			return fmt.Errorf("authentication failed: no token received")
		}
		logger.Info("Final state is valid, has token")

		// Save the token
		token := final.token.AccessToken

		// Debug output
		fmt.Printf("\nDEBUG: Token response received\n")
		fmt.Printf("  AccessToken length: %d\n", len(token))
		fmt.Printf("  TokenType: %s\n", final.token.TokenType)
		fmt.Printf("  ExpiresIn: %d\n", final.token.ExpiresIn)
		fmt.Printf("  RefreshToken length: %d\n", len(final.token.RefreshToken))

		if token == "" {
			logger.Error("Received empty access token")
			return fmt.Errorf("authentication failed: received empty access token")
		}

		logger.Info("Saving token to config", "token_length", len(token))
		if err := config.Set("auth.token", token); err != nil {
			logger.Error("Failed to save token", "error", err)
			return fmt.Errorf("failed to save authentication token: %w", err)
		}
		logger.Info("Token saved successfully")

		// Verify it was saved
		savedToken, ok := config.Get("auth.token")
		if ok && savedToken != "" {
			fmt.Printf("DEBUG: Token saved successfully, length: %d\n", len(savedToken.(string)))
		} else {
			fmt.Println("DEBUG: Failed to verify saved token")
		}

		fmt.Println()
		fmt.Println(successStyle.Render("✓ Authentication successful!"))
		fmt.Println()

		return nil
	},
}

// Auth spinner model
type authSpinnerModel struct {
	spinner  int
	token    *controlplane.TokenResponse
	err      error
	quitting bool
}

func NewAuthSpinner() authSpinnerModel {
	return authSpinnerModel{}
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type authSuccessMsg struct {
	token *controlplane.TokenResponse
}

type authErrorMsg struct {
	err error
}

type tickMsg time.Time

func (m authSpinnerModel) Init() tea.Cmd {
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m authSpinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			m.err = fmt.Errorf("authentication cancelled by user")
			return m, tea.Quit
		}

	case tickMsg:
		if !m.quitting {
			m.spinner = (m.spinner + 1) % len(spinnerFrames)
			return m, tick()
		}

	case authSuccessMsg:
		fmt.Println("\nDEBUG: authSuccessMsg received in Update")
		m.token = msg.token
		m.quitting = true
		return m, tea.Quit

	case authErrorMsg:
		fmt.Printf("\nDEBUG: authErrorMsg received in Update: %v\n", msg.err)
		m.err = msg.err
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

func (m authSpinnerModel) View() string {
	if m.quitting {
		return ""
	}

	frame := spinnerFrames[m.spinner]
	return fmt.Sprintf("%s Waiting for authorization...", frame)
}

// `ufctl auth logout` clears the authentication token
var AuthLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Logout and clear authentication token",
	Long:  "Removes the stored authentication token from local configuration.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		config := deps.ConfigClient

		// Clear the token
		if err := config.Set("auth.token", ""); err != nil {
			return fmt.Errorf("failed to clear authentication token: %w", err)
		}

		fmt.Println(successStyle.Render("✓ Logged out successfully"))
		return nil
	},
}

// `ufctl auth status` shows current authentication status
var AuthStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show authentication status",
	Long:  "Display current authentication status and token information.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		config := deps.ConfigClient

		token, ok := config.Get("auth.token")

		if !ok || token == nil || token == "" {
			fmt.Println(errorStyle.Render("✗ Not authenticated"))
			fmt.Println()
			fmt.Println(instructionStyle.Render("Run 'ufctl local auth login' to authenticate"))
			return nil
		}

		fmt.Println(successStyle.Render("✓ Authenticated"))
		fmt.Println()
		fmt.Println(instructionStyle.Render("Token is configured and ready to use"))
		return nil
	},
}

func init() {
	AuthLoginCmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Don't open browser automatically")
	AuthLoginCmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Timeout for authorization")

	AuthCmd.AddCommand(AuthLoginCmd)
	AuthCmd.AddCommand(AuthLogoutCmd)
	AuthCmd.AddCommand(AuthStatusCmd)
}
