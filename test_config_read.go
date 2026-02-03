package main

import (
	"fmt"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
)

func main() {
	// Set config path
	os.Setenv("UNDERLEAF_CONFIG_PATH", "test_cluster/config_node1.yaml")

	// Create config client
	config := policy_manager.NewCLIConfigClient()

	// Try to read various keys
	keys := []string{
		"local.server_id",
		"local.auth.token",
		"local.raft.enabled",
		"local.raft.node_id",
		"local.mdns.enabled",
		"local.mdns.cluster_id",
	}

	fmt.Println("Config values:")
	for _, key := range keys {
		if val, ok := config.Get(key); ok {
			fmt.Printf("  %s = %v\n", key, val)
		} else {
			fmt.Printf("  %s = <not found>\n", key)
		}
	}
}
