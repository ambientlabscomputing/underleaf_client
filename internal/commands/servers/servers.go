package servers

import "github.com/spf13/cobra"

var ServersCmd = &cobra.Command{
	Use:   "servers",
	Short: "Manage Underleaf servers",
	Long:  "Commands to manage and interact with Underleaf servers.",
}

func init() {
	ServersCmd.AddCommand(CreateCmd)
	ServersCmd.AddCommand(SSHKeysCmd)
	ServersCmd.AddCommand(ListCmd)
	ServersCmd.AddCommand(StatusCmd)
	ServersCmd.AddCommand(DescribeCmd)
	ServersCmd.AddCommand(UpdateCmd)
	ServersCmd.AddCommand(MetricsCmd)
	ServersCmd.AddCommand(ActivityCmd)
	ServersCmd.AddCommand(ExecCmd)
	ServersCmd.AddCommand(LogsCmd)
}
