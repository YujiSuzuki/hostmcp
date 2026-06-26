package cli

import (
	_ "embed"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

//go:embed configs/hostmcp.example.yaml
var exampleConfig []byte

var (
	initWorkspace string
	initForce     bool
	initPort      int
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate hostmcp.yaml config from built-in template",
	Long: `Generate a hostmcp.yaml configuration file in {workspace}/.sandbox/config/
from the built-in template.

Example:
  hostmcp init --workspace ~/projects/my-app`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVar(&initWorkspace, "workspace", "", "Target workspace directory (required)")
	initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite existing config file")
	initCmd.Flags().IntVar(&initPort, "port", 0, "Port number to set in generated config (default: uses template default 18080)")
	_ = initCmd.MarkFlagRequired("workspace")
}

func runInit(cmd *cobra.Command, args []string) error {
	absWorkspace, err := filepath.Abs(initWorkspace)
	if err != nil {
		return fmt.Errorf("invalid workspace path: %w", err)
	}

	configDir := filepath.Join(absWorkspace, ".sandbox", "config")
	configPath := filepath.Join(configDir, "hostmcp.yaml")

	if _, statErr := os.Stat(configPath); statErr == nil {
		if !initForce {
			return fmt.Errorf("config already exists: %s\nUse --force to overwrite", configPath)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("failed to check config file: %w", statErr)
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configBytes := exampleConfig
	if initPort != 0 {
		if initPort < 1 || initPort > 65535 {
			return fmt.Errorf("invalid port %d: must be between 1 and 65535", initPort)
		}
		var err error
		configBytes, err = substituteServerPort(exampleConfig, initPort)
		if err != nil {
			return err
		}
	}

	if err := os.WriteFile(configPath, configBytes, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Created: %s\n\n", configPath)
	fmt.Println("Edit the file to configure containers and permissions.")
	fmt.Printf("Then run:\n  hostmcp serve --workspace %s\n", absWorkspace)
	return nil
}

// substituteServerPort parses content as YAML, sets server.port to port,
// and returns the re-encoded YAML with comments preserved.
func substituteServerPort(content []byte, port int) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse config template: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("config template is empty")
	}

	serverNode := yamlMappingValue(doc.Content[0], "server")
	if serverNode == nil {
		return nil, fmt.Errorf("config template does not contain 'server' section")
	}
	portNode := yamlMappingValue(serverNode, "port")
	if portNode == nil {
		return nil, fmt.Errorf("config template does not contain 'server.port'")
	}

	portNode.Value = strconv.Itoa(port)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("failed to encode config: %w", err)
	}
	return buf.Bytes(), nil
}

// yamlMappingValue returns the value node for key in a YAML mapping node.
func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
