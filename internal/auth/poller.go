package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
)

// PollerConfig holds configuration for the token poller
type PollerConfig struct {
	InitialInterval time.Duration // Initial polling interval
	MaxInterval     time.Duration // Maximum polling interval after slow_down
	Timeout         time.Duration // Overall timeout for the flow
}

// DefaultPollerConfig returns sensible defaults for polling
func DefaultPollerConfig() PollerConfig {
	return PollerConfig{
		InitialInterval: 5 * time.Second,
		MaxInterval:     10 * time.Second,
		Timeout:         5 * time.Minute,
	}
}

// PollResult represents the result of a polling operation
type PollResult struct {
	Token *controlplane.TokenResponse
	Error error
}

// Poller handles the token polling loop with proper backoff and timeout
type Poller struct {
	authClient *controlplane.CPlaneAuthClient
	config     PollerConfig
}

// NewPoller creates a new token poller
func NewPoller(authClient *controlplane.CPlaneAuthClient, config PollerConfig) *Poller {
	return &Poller{
		authClient: authClient,
		config:     config,
	}
}

// Poll starts polling for the token and returns a channel with results
// The channel will receive exactly one result (success or failure) and then close
func (p *Poller) Poll(ctx context.Context, deviceCode string) <-chan PollResult {
	resultChan := make(chan PollResult, 1)

	go func() {
		fmt.Printf("DEBUG [poller]: Starting polling goroutine for device_code=%s\n", deviceCode[:8]+"...")
		defer close(resultChan)
		defer fmt.Println("DEBUG [poller]: Polling goroutine exiting")

		// Create a timeout context
		timeoutCtx, cancel := context.WithTimeout(ctx, p.config.Timeout)
		defer cancel()

		interval := p.config.InitialInterval
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		fmt.Printf("DEBUG [poller]: Initial interval=%v, timeout=%v\n", interval, p.config.Timeout)

		for {
			select {
			case <-timeoutCtx.Done():
				fmt.Println("DEBUG [poller]: Timeout reached")
				resultChan <- PollResult{
					Error: fmt.Errorf("authentication timeout: user did not authorize within %v", p.config.Timeout),
				}
				return

			case <-ticker.C:
				fmt.Println("DEBUG [poller]: Tick - polling for token")
				token, err := p.authClient.PollForToken(timeoutCtx, deviceCode)

				if err == nil {
					// Success! We got the token
					fmt.Printf("DEBUG [poller]: SUCCESS - Got token (length=%d)\n", len(token.AccessToken))
					fmt.Println("DEBUG [poller]: Sending success to result channel")
					resultChan <- PollResult{Token: token}
					fmt.Println("DEBUG [poller]: Success sent, returning")
					return
				}

				// Check if it's a TokenError we can handle
				fmt.Printf("DEBUG [poller]: Received error: %v\n", err)
				var tokenErr *controlplane.TokenError
				if errors.As(err, &tokenErr) {
					fmt.Printf("DEBUG [poller]: TokenError code=%s\n", tokenErr.Code)
					if tokenErr.IsAuthorizationPending() {
						// User hasn't authorized yet, keep polling
						fmt.Println("DEBUG [poller]: Authorization pending, continuing to poll")
						continue
					}

					if tokenErr.IsSlowDown() {
						// Server wants us to slow down
						fmt.Printf("DEBUG [poller]: Slow down requested, increasing interval to %v\n", p.config.MaxInterval)
						interval = p.config.MaxInterval
						ticker.Reset(interval)
						continue
					}

					if tokenErr.IsExpired() {
						fmt.Println("DEBUG [poller]: Device code expired")
						resultChan <- PollResult{
							Error: fmt.Errorf("device code expired: please try again"),
						}
						return
					}

					if tokenErr.IsAccessDenied() {
						fmt.Println("DEBUG [poller]: Access denied")
						resultChan <- PollResult{
							Error: fmt.Errorf("authorization denied: user rejected the request"),
						}
						return
					}
				}

				// Some other error occurred
				fmt.Printf("DEBUG [poller]: Unexpected error, sending to result channel: %v\n", err)
				resultChan <- PollResult{Error: err}
				return
			}
		}
	}()

	return resultChan
}
