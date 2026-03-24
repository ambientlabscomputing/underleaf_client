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
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	internalutils "github.com/ambientlabscomputing/underleaf_client/internal/utils"
)

var (
	noBrowser bool
	timeout   time.Duration
	useSSH    bool
	serverID  string
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
	Short: "Login using device authorization or SSH",
	Long:  "Authenticate using OAuth2 device code flow or SSH authentication.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check if using SSH auth
		if useSSH {
			return runSSHAuth(cmd, serverID)
		}

		// Otherwise, run device code auth
		return runDeviceCodeAuth(cmd)
	},
}

// runDeviceCodeAuth handles the standard OAuth2 device code flow
func runDeviceCodeAuth(cmd *cobra.Command) error {
	ctx := cmd.Context()
	logger := logging.GetLogger(ctx)
	printer := ui.GetPrinter(ctx)

	// Get dependencies
	deps := utils.NewDependencyManager(ctx)
	if deps == nil {
		return fmt.Errorf("failed to create dependencies")
	}

	config := deps.ConfigClient
	authClient := deps.CPlaneClient.Auth

	// Start the device authorization flow
	printer.Print(titleStyle.Render("🔐 Underleaf Authentication"))
	printer.Print("")
	printer.Print("Initiating device authorization flow...")
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
	printer.Print("")
	printer.Print("Please authorize this device by visiting:")
	printer.Print("")
	printer.Print("  " + urlStyle.Render(displayURL))
	printer.Print("")

	// Only show the code if we're using the basic verification URI
	if authResp.VerificationURIComplete == "" {
		printer.Print("And enter the code:")
		printer.Print("")
		printer.Print("  " + codeStyle.Render(authResp.UserCode))
		printer.Print("")
	}

	// Open browser unless --no-browser flag is set
	if !noBrowser {
		if err := internalutils.OpenURL(displayURL); err != nil {
			printer.PrintWarning(fmt.Sprintf("Could not open browser: %v", err))
			printer.Print(instructionStyle.Render("Please open the URL manually."))
		} else {
			printer.PrintSuccess("Browser opened automatically")
		}
	}

	printer.Print("")
	printer.Print(instructionStyle.Render(fmt.Sprintf("Code expires in %d seconds", authResp.ExpiresIn)))
	printer.Print(instructionStyle.Render("Waiting for authorization..."))
	printer.Print("")

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

	printer.Print("")
	printer.PrintSuccess("Authentication successful!")
	printer.Print("")

	// Auto-provision organization for simplified onboarding
	userClient := deps.CPlaneClient.Users
	logger.Info("Auto-provisioning organization for new user")

	org, err := userClient.AutoProvisionOrganization(ctx)
	if err != nil {
		logger.Warn("Failed to auto-provision organization", "error", err)
		printer.PrintWarning("Could not auto-provision organization. You can create one manually later.")
		return nil
	}

	// Save the auto-provisioned org context
	logger.Info("Auto-provisioned organization", "org_id", org.ID, "org_name", org.Name, "org_slug", org.Slug)
	if err := config.Set("local.organization_id", org.ID); err != nil {
		logger.Error("Failed to save organization ID", "error", err)
	}
	if err := config.Set("local.organization_name", org.Name); err != nil {
		logger.Error("Failed to save organization name", "error", err)
	}
	if err := config.Set("local.organization_slug", org.Slug); err != nil {
		logger.Error("Failed to save organization slug", "error", err)
	}

	printer.PrintSuccess("Organization set: " + org.Name)
	printer.Print(fmt.Sprintf("  Organization ID: %s", org.ID))

	return nil
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

// Org picker model
type orgPickerModel struct {
	orgs        []controlplane.OrgMembership
	cursor      int
	selectedOrg *controlplane.OrgMembership
	cancelled   bool
}

func NewOrgPicker(orgs []controlplane.OrgMembership) orgPickerModel {
	return orgPickerModel{
		orgs:   orgs,
		cursor: 0,
	}
}

func (m orgPickerModel) Init() tea.Cmd {
	return nil
}

func (m orgPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.cancelled = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.orgs)-1 {
				m.cursor++
			}

		case "enter":
			m.selectedOrg = &m.orgs[m.cursor]
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m orgPickerModel) View() string {
	if m.cancelled {
		return ""
	}

	s := ""
	for i, org := range m.orgs {
		cursor := " "
		if m.cursor == i {
			cursor = "▶"
			s += fmt.Sprintf("%s %s\n",
				currentOrgStyle.Render(cursor),
				orgNameStyle.Render(org.Name+" ("+org.Role+")"))
		} else {
			s += fmt.Sprintf("%s %s\n", cursor, org.Name+" ("+org.Role+")")
		}
		s += fmt.Sprintf("  %s\n\n", orgIDStyle.Render("ID: "+org.ID))
	}

	return s
}

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
		m.token = msg.token
		m.quitting = true
		return m, tea.Quit

	case authErrorMsg:
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

		printer := ui.GetPrinter(ctx)
		printer.PrintSuccess("Logged out successfully")
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

		printer := ui.GetPrinter(ctx)
		if !ok || token == nil || token == "" {
			printer.PrintError("Not authenticated")
			printer.Print("Run 'ufctl auth login' to authenticate")
			return nil
		}

		printer.PrintSuccess("Authenticated")
		printer.Print("Token is configured and ready to use")
		return nil
	},
}

func init() {
	AuthLoginCmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Don't open browser automatically")
	AuthLoginCmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Timeout for authorization")
	AuthLoginCmd.Flags().BoolVar(&useSSH, "use-ssh", false, "Use SSH authentication instead of device code flow")
	AuthLoginCmd.Flags().StringVar(&serverID, "server-id", "", "Server ID for SSH authentication (required with --use-ssh)")

	AuthCmd.AddCommand(AuthLoginCmd)
	AuthCmd.AddCommand(AuthLogoutCmd)
	AuthCmd.AddCommand(AuthStatusCmd)
}
