package cluster

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var kvCmd = &cobra.Command{
	Use:   "kv",
	Short: "Interact with cluster KV store",
	Long:  `Interact with the Raft-backed key-value store. Supports get, put, delete, and list operations.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var kvGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a value from the KV store",
	Long:  `Retrieve a value from the cluster KV store by key.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		key := args[0]
		if !strings.HasPrefix(key, "/") {
			key = "/" + key
		}

		// Create agent client with effective port
		effectivePort := getEffectiveAgentPort(cmd, agentPort)
		client := agent.NewClient(effectivePort)

		// Get value
		resp, err := client.DoRequest("GET", fmt.Sprintf("/api/v1/raft/kv%s", key), nil)
		if err != nil {
			logger.Error("failed to get key", "error", err, "key", key)
			return fmt.Errorf("failed to get key: %w", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(resp, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		printer.PrintSuccess(fmt.Sprintf("Key: %v", result["key"]))
		printer.Print(fmt.Sprintf("Value: %v", result["value"]))
		printer.Print(fmt.Sprintf("Revision: %v", result["revision"]))
		printer.Print(fmt.Sprintf("Version: %v", result["version"]))

		return nil
	},
}

var kvPutCmd = &cobra.Command{
	Use:   "put <key> <value>",
	Short: "Put a value in the KV store",
	Long:  `Store a key-value pair in the cluster KV store.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		key := args[0]
		value := args[1]

		if !strings.HasPrefix(key, "/") {
			key = "/" + key
		}

		// Create agent client with effective port
		effectivePort := getEffectiveAgentPort(cmd, agentPort)
		client := agent.NewClient(effectivePort)

		// Put value
		payload := map[string]string{
			"value": value,
		}
		payloadBytes, _ := json.Marshal(payload)

		_, err := client.DoRequest("PUT", fmt.Sprintf("/api/v1/raft/kv%s", key), payloadBytes)
		if err != nil {
			logger.Error("failed to put key", "error", err, "key", key)
			return fmt.Errorf("failed to put key: %w", err)
		}

		printer.PrintSuccess(fmt.Sprintf("Successfully stored key: %s", key))

		return nil
	},
}

var kvDeleteCmd = &cobra.Command{
	Use:   "delete <key>",
	Short: "Delete a value from the KV store",
	Long:  `Delete a key-value pair from the cluster KV store.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		key := args[0]
		if !strings.HasPrefix(key, "/") {
			key = "/" + key
		}

		// Create agent client with effective port
		effectivePort := getEffectiveAgentPort(cmd, agentPort)
		client := agent.NewClient(effectivePort)

		// Delete value
		_, err := client.DoRequest("DELETE", fmt.Sprintf("/api/v1/raft/kv%s", key), nil)
		if err != nil {
			logger.Error("failed to delete key", "error", err, "key", key)
			return fmt.Errorf("failed to delete key: %w", err)
		}

		printer.PrintSuccess(fmt.Sprintf("Successfully deleted key: %s", key))

		return nil
	},
}

var kvListCmd = &cobra.Command{
	Use:   "list [prefix]",
	Short: "List keys in the KV store",
	Long:  `List all keys with an optional prefix filter.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		prefix := "/"
		if len(args) > 0 {
			prefix = args[0]
			if !strings.HasPrefix(prefix, "/") {
				prefix = "/" + prefix
			}
		}

		// Create agent client with effective port
		effectivePort := getEffectiveAgentPort(cmd, agentPort)
		client := agent.NewClient(effectivePort)

		// List keys
		resp, err := client.DoRequest("GET", fmt.Sprintf("/api/v1/raft/kv?prefix=%s", prefix), nil)
		if err != nil {
			logger.Error("failed to list keys", "error", err, "prefix", prefix)
			return fmt.Errorf("failed to list keys: %w", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(resp, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		count := result["count"]
		entries := result["entries"].([]interface{})

		printer.PrintSuccess(fmt.Sprintf("Found %v keys with prefix: %s", count, prefix))

		if len(entries) > 0 {
			printer.Print("\nKeys:")
			for _, entry := range entries {
				if entryMap, ok := entry.(map[string]interface{}); ok {
					printer.Print(fmt.Sprintf("  %v = %v (rev: %v, ver: %v)",
						entryMap["key"],
						entryMap["value"],
						entryMap["revision"],
						entryMap["version"]))
				}
			}
		}

		return nil
	},
}

func init() {
	// Add KV subcommands
	kvCmd.AddCommand(kvGetCmd)
	kvCmd.AddCommand(kvPutCmd)
	kvCmd.AddCommand(kvDeleteCmd)
	kvCmd.AddCommand(kvListCmd)

	// Add common flags
	kvGetCmd.Flags().IntVarP(&agentPort, "port", "p", 8080, "Agent port")
	kvPutCmd.Flags().IntVarP(&agentPort, "port", "p", 8080, "Agent port")
	kvDeleteCmd.Flags().IntVarP(&agentPort, "port", "p", 8080, "Agent port")
	kvListCmd.Flags().IntVarP(&agentPort, "port", "p", 8080, "Agent port")
}
