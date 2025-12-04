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
		defer close(resultChan)

		// Create a timeout context
		timeoutCtx, cancel := context.WithTimeout(ctx, p.config.Timeout)
		defer cancel()

		interval := p.config.InitialInterval
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-timeoutCtx.Done():
				resultChan <- PollResult{
					Error: fmt.Errorf("authentication timeout: user did not authorize within %v", p.config.Timeout),
				}
				return

			case <-ticker.C:
				token, err := p.authClient.PollForToken(timeoutCtx, deviceCode)

				if err == nil {
					// Success! We got the token
					resultChan <- PollResult{Token: token}
					return
				}

				// Check if it's a TokenError we can handle
				var tokenErr *controlplane.TokenError
				if errors.As(err, &tokenErr) {
					if tokenErr.IsAuthorizationPending() {
						// User hasn't authorized yet, keep polling
						continue
					}

					if tokenErr.IsSlowDown() {
						// Server wants us to slow down
						interval = p.config.MaxInterval
						ticker.Reset(interval)
						continue
					}

					if tokenErr.IsExpired() {
						resultChan <- PollResult{
							Error: fmt.Errorf("device code expired: please try again"),
						}
						return
					}

					if tokenErr.IsAccessDenied() {
						resultChan <- PollResult{
							Error: fmt.Errorf("authorization denied: user rejected the request"),
						}
						return
					}
				}

				// Some other error occurred
				resultChan <- PollResult{Error: err}
				return
			}
		}
	}()

	return resultChan
}
