package cli

import (
	"testing"
)

func TestFormatFlag_UsesUpperCaseF(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("format")
	if f == nil {
		t.Fatal("--format flag not registered on rootCmd")
	}
	if f.Shorthand != "F" {
		t.Errorf("--format shorthand = %q, want %q (upper-case to avoid collision with --follow -f)", f.Shorthand, "F")
	}
}

func TestFormatFlag_LowerF_NotRegistered(t *testing.T) {
	// Ensure no persistent flag claims the shorthand "-f" on rootCmd,
	// so subcommands (logs --follow -f) can register it locally.
	f := rootCmd.PersistentFlags().ShorthandLookup("f")
	if f != nil {
		t.Errorf("shorthand -f is registered on rootCmd as --%s; this collides with logs --follow -f", f.Name)
	}
}
