package local

import (
	"github.com/spf13/cobra"
)

// CSRCmd is the root command for CSR (Certificate Signing Request) operations
var CSRCmd = &cobra.Command{
	Use:   "csr",
	Short: "Manage mTLS Certificate Signing Requests",
	Long:  "Commands to generate and submit Certificate Signing Requests for mTLS authentication.",
}

func init() {
	CSRCmd.AddCommand(GenerateCSRCmd)
	CSRCmd.AddCommand(SubmitCSRCmd)
}
