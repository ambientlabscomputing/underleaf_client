package secrets

import (
	"github.com/spf13/cobra"
)

// SecretsCmd is the root command for secret management.
var SecretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Manage encrypted secrets",
	Long: `Manage encrypted secrets with edge-native storage and cross-cluster replication.

Secrets are stored encrypted at rest on the cluster's Raft KV store. The cloud
control plane tracks only metadata -- no plaintext or keys are ever sent to the cloud.

Subcommands:
  add      Store a new secret on the local cluster
  list     List secrets from the control plane
  get      Retrieve a secret value from the local agent
  delete   Revoke a secret
  rotate   Update a secret to a new value
  status   Show per-cluster replication status
  vault    Manage the local secret vault lifecycle`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	SecretsCmd.AddCommand(addCmd)
	SecretsCmd.AddCommand(listCmd)
	SecretsCmd.AddCommand(getCmd)
	SecretsCmd.AddCommand(deleteCmd)
	SecretsCmd.AddCommand(rotateCmd)
	SecretsCmd.AddCommand(statusCmd)
	SecretsCmd.AddCommand(sealCmd)
}
