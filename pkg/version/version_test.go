package version_test

import (
"testing"

"github.com/ambientlabscomputing/underleaf_client/pkg/version"
)

func TestVersionDefault_NonEmpty(t *testing.T) {
if version.Version == "" {
t.Error("Version must not be empty; default should be '0.0.0'")
}
}

func TestVersionDefault_IsDevBuild(t *testing.T) {
// When compiled without ldflags, Version uses the in-code default.
// Production CI overrides via:
//   -X github.com/ambientlabscomputing/underleaf_client/pkg/version.Version=v1.2.3
if version.Version != "0.0.0" {
t.Logf("Version overridden to %q (production/dev build)", version.Version)
}
}

func TestVersionFields_Exist(t *testing.T) {
// Verify the package surface: all three vars must exist.
_ = version.Version
_ = version.GitCommit
_ = version.BuildDate
}
