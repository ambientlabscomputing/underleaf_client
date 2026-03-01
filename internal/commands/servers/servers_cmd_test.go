package servers_test

import (
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/servers"
)

// subcommandNames returns the Use fields of all subcommands registered on ServersCmd.
func subcommandNames() []string {
	cmds := servers.ServersCmd.Commands()
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name())
	}
	return names
}

func containsName(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}

// --- ServersCmd root ---

func TestServersCmd_UseIsServers(t *testing.T) {
	if servers.ServersCmd.Use != "servers" {
		t.Errorf("ServersCmd.Use = %q, want servers", servers.ServersCmd.Use)
	}
}

func TestServersCmd_HasShortDescription(t *testing.T) {
	if servers.ServersCmd.Short == "" {
		t.Error("ServersCmd.Short must not be empty")
	}
}

// --- Subcommands registered ---

func TestServersCmd_HasListSubcommand(t *testing.T) {
	if !containsName(subcommandNames(), "list") {
		t.Error("ServersCmd missing 'list' subcommand")
	}
}

func TestServersCmd_HasDescribeSubcommand(t *testing.T) {
	if !containsName(subcommandNames(), "describe") {
		t.Error("ServersCmd missing 'describe' subcommand")
	}
}

func TestServersCmd_HasExecSubcommand(t *testing.T) {
	if !containsName(subcommandNames(), "exec") {
		t.Error("ServersCmd missing 'exec' subcommand")
	}
}

func TestServersCmd_HasStatusSubcommand(t *testing.T) {
	if !containsName(subcommandNames(), "status") {
		t.Error("ServersCmd missing 'status' subcommand")
	}
}

func TestServersCmd_HasActivitySubcommand(t *testing.T) {
	if !containsName(subcommandNames(), "activity") {
		t.Error("ServersCmd missing 'activity' subcommand")
	}
}

func TestServersCmd_HasLogsSubcommand(t *testing.T) {
	if !containsName(subcommandNames(), "logs") {
		t.Error("ServersCmd missing 'logs' subcommand")
	}
}

func TestServersCmd_HasUpdateSubcommand(t *testing.T) {
	if !containsName(subcommandNames(), "update") {
		t.Error("ServersCmd missing 'update' subcommand")
	}
}

// --- ExecCmd flags ---

func TestExecCmd_HasTimeoutFlag(t *testing.T) {
	f := servers.ExecCmd.Flags().Lookup("timeout")
	if f == nil {
		t.Fatal("ExecCmd missing --timeout flag")
	}
}

func TestExecCmd_TimeoutDefaultIsZero(t *testing.T) {
	f := servers.ExecCmd.Flags().Lookup("timeout")
	if f == nil {
		t.Skip("--timeout flag not found")
	}
	if f.DefValue != "0" {
		t.Errorf("--timeout default = %q, want 0 (0 means use server default)", f.DefValue)
	}
}

func TestExecCmd_HasEnvFlag(t *testing.T) {
	if servers.ExecCmd.Flags().Lookup("env") == nil {
		t.Error("ExecCmd missing --env flag")
	}
}

func TestExecCmd_HasDetachFlag(t *testing.T) {
	f := servers.ExecCmd.Flags().Lookup("detach")
	if f == nil {
		t.Fatal("ExecCmd missing --detach flag")
	}
	if f.DefValue != "false" {
		t.Errorf("--detach default = %q, want false", f.DefValue)
	}
}

// --- ListCmd flags ---

func TestListCmd_HasStatusFlag(t *testing.T) {
	if servers.ListCmd.Flags().Lookup("status") == nil {
		t.Error("ListCmd missing --status flag")
	}
}

func TestListCmd_HasLimitFlag(t *testing.T) {
	f := servers.ListCmd.Flags().Lookup("limit")
	if f == nil {
		t.Fatal("ListCmd missing --limit flag")
	}
	if f.DefValue != "50" {
		t.Errorf("--limit default = %q, want 50", f.DefValue)
	}
}

func TestListCmd_HasOffsetFlag(t *testing.T) {
	f := servers.ListCmd.Flags().Lookup("offset")
	if f == nil {
		t.Fatal("ListCmd missing --offset flag")
	}
	if f.DefValue != "0" {
		t.Errorf("--offset default = %q, want 0", f.DefValue)
	}
}

// --- DescribeCmd args ---

func TestDescribeCmd_RequiresExactlyOneArg(t *testing.T) {
	// Validate via cobra's Args validator.
	if servers.DescribeCmd.Args == nil {
		t.Fatal("DescribeCmd.Args validator is nil — must require exactly one arg")
	}
	// cobra.ExactArgs(1) returns an error when given 0 args.
	if err := servers.DescribeCmd.Args(servers.DescribeCmd, []string{}); err == nil {
		t.Error("DescribeCmd.Args should fail with 0 arguments")
	}
	if err := servers.DescribeCmd.Args(servers.DescribeCmd, []string{"id1"}); err != nil {
		t.Errorf("DescribeCmd.Args should pass with 1 argument: %v", err)
	}
}

// --- ActivityCmd flags ---

func TestActivityCmd_HasTypeFlag(t *testing.T) {
	if servers.ActivityCmd.Flags().Lookup("type") == nil {
		t.Error("ActivityCmd missing --type flag")
	}
}

// --- UpdateCmd args ---

func TestUpdateCmd_RequiresExactlyOneArg(t *testing.T) {
	if servers.UpdateCmd.Args == nil {
		t.Fatal("UpdateCmd.Args validator is nil")
	}
	if err := servers.UpdateCmd.Args(servers.UpdateCmd, []string{}); err == nil {
		t.Error("UpdateCmd.Args should fail with 0 arguments")
	}
	if err := servers.UpdateCmd.Args(servers.UpdateCmd, []string{"id1"}); err != nil {
		t.Errorf("UpdateCmd.Args should pass with 1 argument: %v", err)
	}
}
