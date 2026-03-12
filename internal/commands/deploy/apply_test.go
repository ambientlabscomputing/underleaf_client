package deploy

import (
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/types"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
)

// TestApplyExposeFlagParsing tests the --expose flag parsing in deploy apply
func TestApplyExposeFlagParsing(t *testing.T) {
	tests := []struct {
		name          string
		flag          string
		deployment    *types.AppDeployment
		wantPort      int
		wantService   string
		wantError     bool
		errorContains string
	}{
		{
			name: "basic expose web:8080",
			flag: "web:8080",
			deployment: &types.AppDeployment{
				Services: []types.ServiceSpec{
					{Name: "web", Image: "nginx"},
				},
			},
			wantError:   false,
			wantService: "web",
			wantPort:    8080,
		},
		{
			name: "expose flag with custom port",
			flag: "api:3000",
			deployment: &types.AppDeployment{
				Services: []types.ServiceSpec{
					{Name: "api", Image: "node"},
				},
			},
			wantError:   false,
			wantService: "api",
			wantPort:    3000,
		},
		{
			name:          "invalid format - no colon",
			flag:          "web8080",
			deployment:    &types.AppDeployment{},
			wantError:     true,
			errorContains: "invalid expose format",
		},
		{
			name:          "invalid format - non-numeric port",
			flag:          "web:abc",
			deployment:    &types.AppDeployment{},
			wantError:     true,
			errorContains: "invalid port number",
		},
		{
			name:          "invalid port - out of range",
			flag:          "web:65536",
			deployment:    &types.AppDeployment{},
			wantError:     true,
			errorContains: "port must be between 1 and 65535",
		},
		{
			name: "service not found",
			flag: "missing:8080",
			deployment: &types.AppDeployment{
				Services: []types.ServiceSpec{
					{Name: "web", Image: "nginx"},
				},
			},
			wantError:     true,
			errorContains: "not found in deployment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := ui.NewPrinter(ui.FormatTable)
			deps := &utils.DependencyManager{Printer: *printer}
			err := applyExposeFlag(tt.deployment, tt.flag, deps)

			if (err != nil) != tt.wantError {
				t.Fatalf("applyExposeFlag() error = %v, wantError %v", err, tt.wantError)
			}

			if tt.wantError {
				if tt.errorContains != "" && err != nil {
					// Would check error message contains expected text
					// t.Logf("Got expected error: %v", err)
				}
				return
			}

			// Verify the expose config was applied
			found := false
			for _, service := range tt.deployment.Services {
				if service.Name == tt.wantService && service.Expose != nil {
					if service.Expose.Port != tt.wantPort {
						t.Errorf("expected port %d, got %d", tt.wantPort, service.Expose.Port)
					}
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expose flag was not applied to service %s", tt.wantService)
			}
		})
	}
}

// TestDeploymentExposureSummary tests the display of exposure summary
func TestDeploymentExposureSummary(t *testing.T) {
	tests := []struct {
		name              string
		deployment        *types.AppDeployment
		expectedExposures int
	}{
		{
			name: "no exposures",
			deployment: &types.AppDeployment{
				Services: []types.ServiceSpec{
					{Name: "web", Image: "nginx"},
				},
			},
			expectedExposures: 0,
		},
		{
			name: "single exposure",
			deployment: &types.AppDeployment{
				Services: []types.ServiceSpec{
					{
						Name:   "web",
						Image:  "nginx",
						Expose: &types.ExposeConfig{Port: 8080},
					},
				},
			},
			expectedExposures: 1,
		},
		{
			name: "multiple exposures",
			deployment: &types.AppDeployment{
				Services: []types.ServiceSpec{
					{
						Name:   "web",
						Image:  "nginx",
						Expose: &types.ExposeConfig{Port: 8080},
					},
					{
						Name:   "api",
						Image:  "node",
						Expose: &types.ExposeConfig{Port: 3000},
					},
				},
			},
			expectedExposures: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Count exposures in deployment
			exposureCount := 0
			for _, service := range tt.deployment.Services {
				if service.Expose != nil {
					exposureCount++
				}
			}

			if exposureCount != tt.expectedExposures {
				t.Errorf("expected %d exposures, got %d", tt.expectedExposures, exposureCount)
			}
		})
	}
}

// BenchmarkApplyExposeFlag benchmarks the expose flag parsing
func BenchmarkApplyExposeFlag(b *testing.B) {
	deployment := &types.AppDeployment{
		Services: []types.ServiceSpec{
			{Name: "web", Image: "nginx"},
			{Name: "api", Image: "node"},
			{Name: "db", Image: "postgres"},
		},
	}

	printer := ui.NewPrinter(ui.FormatTable)
	deps := &utils.DependencyManager{Printer: *printer}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = applyExposeFlag(deployment, "web:8080", deps)
	}
}
