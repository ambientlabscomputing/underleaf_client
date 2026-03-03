package deploy
package deploy

import (
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/types"
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



















































































































































































}	}		_ = applyExposeFlag(deployment, "web:8080", deps)	for i := 0; i < b.N; i++ {	b.ResetTimer()	deps := &utils.DependencyManager{}	}		},			{Name: "db", Image: "postgres"},			{Name: "api", Image: "node"},			{Name: "web", Image: "nginx"},		Services: []types.ServiceSpec{	deployment := &types.AppDeployment{func BenchmarkApplyExposeFlag(b *testing.B) {// BenchmarkApplyExposeFlag benchmarks the expose flag parsing}	}		})			}				t.Errorf("expected %d exposures, got %d", tt.expectedExposures, exposureCount)			if exposureCount != tt.expectedExposures {			}				}					exposureCount++				if service.Expose != nil {			for _, service := range tt.deployment.Services {			exposureCount := 0			// Count exposures in deployment		t.Run(tt.name, func(t *testing.T) {	for _, tt := range tests {	}		},			expectedExposures: 2,			},				},					},						Expose: &types.ExposeConfig{Port: 3000},						Image:  "node",						Name:   "api",					{					},						Expose: &types.ExposeConfig{Port: 8080},						Image:  "nginx",						Name:   "web",					{				Services: []types.ServiceSpec{			deployment: &types.AppDeployment{			name: "multiple exposures",		{		},			expectedExposures: 1,			},				},					},						Expose: &types.ExposeConfig{Port: 8080},						Image:  "nginx",						Name:   "web",					{				Services: []types.ServiceSpec{			deployment: &types.AppDeployment{			name: "single exposure",		{		},			expectedExposures: 0,			},				},					{Name: "web", Image: "nginx"},				Services: []types.ServiceSpec{			deployment: &types.AppDeployment{			name: "no exposures",		{	}{		expectedExposures int		deployment        *types.AppDeployment		name              string	tests := []struct {func TestDeploymentExposureSummary(t *testing.T) {// TestDeploymentExposureSummary tests the display of exposure summary}	}		})			}				t.Errorf("expose flag was not applied to service %s", tt.wantService)			if !found {			}				}					break					found = true					}						t.Errorf("expected port %d, got %d", tt.wantPort, service.Expose.Port)					if service.Expose.Port != tt.wantPort {				if service.Name == tt.wantService && service.Expose != nil {			for _, service := range tt.deployment.Services {			found := false			// Verify the expose config was applied			}				return				}					// t.Logf("Got expected error: %v", err)					// Would check error message contains expected text				if tt.errorContains != "" && err != nil {			if tt.wantError {			}				t.Fatalf("applyExposeFlag() error = %v, wantError %v", err, tt.wantError)			if (err != nil) != tt.wantError {			err := applyExposeFlag(tt.deployment, tt.flag, deps)			deps := &utils.DependencyManager{}			// In real scenarios, this would be populated from context			// Create a minimal dependency manager for testing		t.Run(tt.name, func(t *testing.T) {	for _, tt := range tests {	}		},			errorContains: "not found in deployment",			wantError:     true,			},				},					{Name: "web", Image: "nginx"},				Services: []types.ServiceSpec{			deployment: &types.AppDeployment{			flag:          "missing:8080",			name:          "service not found",		{		},			errorContains: "port must be between 1 and 65535",			wantError:     true,			deployment:    &types.AppDeployment{},			flag:          "web:65536",			name:          "invalid port - out of range",		{		},			errorContains: "invalid port number",			wantError:     true,			deployment:    &types.AppDeployment{},			flag:          "web:abc",			name:          "invalid format - non-numeric port",		{		},			errorContains: "invalid expose format",			wantError:     true,			deployment:    &types.AppDeployment{},			flag:          "web8080",			name:          "invalid format - no colon",		{		},			wantError:   false,			wantService: "api",			wantPort:    3000,			},				},					{Name: "api", Image: "node"},				Services: []types.ServiceSpec{			deployment: &types.AppDeployment{			flag: "api:3000",			name: "expose flag with custom port",		{		},			wantError:   false,			wantService: "web",			wantPort:    8080,			},				},					{Name: "web", Image: "nginx"},				Services: []types.ServiceSpec{			deployment: &types.AppDeployment{			flag: "web:8080",			name: "valid expose flag",