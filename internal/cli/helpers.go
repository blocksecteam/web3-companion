// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"os"
	"strconv"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/config"
)

func firstNonNil(values ...interface{}) interface{} {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func uint64FromInterface(v interface{}) uint64 {
	switch t := v.(type) {
	case uint64:
		return t
	case uint:
		return uint64(t)
	case int:
		if t >= 0 {
			return uint64(t)
		}
	case int64:
		if t >= 0 {
			return uint64(t)
		}
	case float64:
		if t >= 0 {
			return uint64(t)
		}
	case json.Number:
		if n, err := strconv.ParseUint(t.String(), 10, 64); err == nil {
			return n
		}
	case string:
		if n, err := strconv.ParseUint(strings.TrimSpace(t), 0, 64); err == nil {
			return n
		}
	}
	return 0
}

func int64FromInterface(v interface{}) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case uint64:
		if t <= uint64(^uint64(0)>>1) {
			return int64(t)
		}
	case float64:
		return int64(t)
	case json.Number:
		if n, err := strconv.ParseInt(t.String(), 10, 64); err == nil {
			return n
		}
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(t), 0, 64); err == nil {
			return n
		}
	}
	return 0
}

func extractField(data []byte, field string) string {
	var m map[string]interface{}
	json.Unmarshal(data, &m)
	if v, ok := m[field].(string); ok {
		return v
	}
	return ""
}

func versionCmd() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			PrintJSONObjectTo(cmd.OutOrStdout(), map[string]string{"version": version})
		}}
}

func mustGet(path string) []byte {
	data, err := getAPIClient().Get(path)
	if err != nil {
		PrintErrorJSON(err, data)
		os.Exit(1)
	}
	return data
}

func mustPost(path string, body interface{}) []byte {
	data, err := getAPIClient().Post(path, body)
	if err != nil {
		PrintErrorJSON(err, data)
		os.Exit(1)
	}
	return data
}

func mustPut(path string, body interface{}) []byte {
	data, err := getAPIClient().Put(path, body)
	if err != nil {
		PrintErrorJSON(err, data)
		os.Exit(1)
	}
	return data
}

func mustDelete(path string) []byte {
	data, err := getAPIClient().Delete(path)
	if err != nil {
		PrintErrorJSON(err, data)
		os.Exit(1)
	}
	return data
}

func requireContainerClient() {
	if err := RequireContainerClient(getAPIClient()); err != nil {
		PrintErrorJSON(err, nil)
		os.Exit(1)
	}
}

func getAPIClient() *APIClient {
	if apiClient == nil {
		apiClient = NewAPIClient()
	}
	return apiClient
}

func defaultWalletAddress() string {
	cfg, err := config.Load("")
	if err == nil {
		return strings.TrimSpace(cfg.CLI.WalletAddress)
	}
	return ""
}

func configureJSONCommandTree(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		PrintJSONObjectTo(cmd.OutOrStdout(), commandHelpJSON(cmd))
	})
	cmd.SetUsageFunc(func(cmd *cobra.Command) error {
		PrintJSONObjectTo(cmd.OutOrStdout(), commandHelpJSON(cmd))
		return nil
	})
	for _, child := range cmd.Commands() {
		configureJSONCommandTree(child)
	}
}

func commandHelpJSON(cmd *cobra.Command) map[string]interface{} {
	payload := map[string]interface{}{
		"command": cmd.CommandPath(),
		"use":     cmd.UseLine(),
		"short":   cmd.Short,
	}
	if strings.TrimSpace(cmd.Long) != "" {
		payload["long"] = cmd.Long
	}
	if len(cmd.Aliases) > 0 {
		payload["aliases"] = cmd.Aliases
	}
	if commands := visibleSubcommands(cmd); len(commands) > 0 {
		payload["commands"] = commands
	}
	if flags := visibleFlags(cmd); len(flags) > 0 {
		payload["flags"] = flags
	}
	return payload
}

func visibleSubcommands(cmd *cobra.Command) []map[string]string {
	out := make([]map[string]string, 0)
	for _, child := range cmd.Commands() {
		if child.Hidden {
			continue
		}
		out = append(out, map[string]string{
			"name":  child.Name(),
			"use":   child.UseLine(),
			"short": child.Short,
		})
	}
	return out
}

func visibleFlags(cmd *cobra.Command) []map[string]string {
	out := make([]map[string]string, 0)
	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		out = append(out, map[string]string{
			"name":    flag.Name,
			"default": flag.DefValue,
			"usage":   flag.Usage,
		})
	})
	cmd.InheritedFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		out = append(out, map[string]string{
			"name":    flag.Name,
			"default": flag.DefValue,
			"usage":   flag.Usage,
		})
	})
	return out
}
