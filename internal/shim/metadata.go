package shim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Metadata describes how a shim was created.
type Metadata struct {
	BinaryName        string   `json:"binary_name"`
	InstallCommand    string   `json:"source_install_command"`
	InstallMode       string   `json:"install_mode"`
	InstallScriptPath string   `json:"install_script_path,omitempty"`
	InstallScriptArgs []string `json:"install_script_args,omitempty"`
	BaseImage         string   `json:"base_image"`
	OutputImage       string   `json:"output_image"`
	InstalledAt       string   `json:"installed_timestamp"`
	InstallForceUsed  bool     `json:"install_force"`
}

func (g *Generator) metadataDir() string {
	return filepath.Join(g.config.BaseDir, "metadata")
}

// MetadataPath returns the path for a shim metadata file.
func (g *Generator) MetadataPath(binaryName string) string {
	return filepath.Join(g.metadataDir(), binaryName+".json")
}

// SaveMetadata writes shim metadata to disk for lifecycle operations.
func (g *Generator) SaveMetadata(metadata Metadata) error {
	if err := os.MkdirAll(g.metadataDir(), 0o755); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	path := g.MetadataPath(metadata.BinaryName)
	payload, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return os.WriteFile(path, payload, 0o644)
}

// LoadMetadata reads shim metadata from disk.
func (g *Generator) LoadMetadata(binaryName string) (Metadata, error) {
	var metadata Metadata

	payload, err := os.ReadFile(g.MetadataPath(binaryName))
	if err != nil {
		return metadata, err
	}
	if err := json.Unmarshal(payload, &metadata); err != nil {
		return metadata, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return metadata, nil
}

// RemoveMetadata deletes the metadata file for a shim.
func (g *Generator) RemoveMetadata(binaryName string) error {
	return os.Remove(g.MetadataPath(binaryName))
}

func (g *Generator) ListAllMetadata() ([]Metadata, error) {
	entries, err := os.ReadDir(g.metadataDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []Metadata{}, nil
		}
		return nil, fmt.Errorf("failed to read metadata directory: %w", err)
	}

	metadataList := make([]Metadata, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		binaryName := strings.TrimSuffix(entry.Name(), ".json")
		metadata, err := g.LoadMetadata(binaryName)
		if err != nil {
			return nil, fmt.Errorf("failed to load metadata for %q: %w", binaryName, err)
		}
		metadataList = append(metadataList, metadata)
	}

	return metadataList, nil
}

func (g *Generator) RemoveAllMetadata() ([]string, error) {
	entries, err := os.ReadDir(g.metadataDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read metadata directory: %w", err)
	}

	removed := make([]string, 0, len(entries))
	var firstErr error
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		binaryName := strings.TrimSuffix(entry.Name(), ".json")
		if err := os.Remove(filepath.Join(g.metadataDir(), entry.Name())); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to remove metadata for %q: %w", binaryName, err)
			}
			continue
		}
		removed = append(removed, binaryName)
	}

	return removed, firstErr
}
