package exec

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

func configurationFingerprint(project, configuration string) string {
	if validatorConfiguration(project) != configuration {
		return "changed"
	}
	if configuration == "" {
		return "absent"
	}
	raw, err := os.ReadFile(filepath.Clean(configuration))
	if err != nil {
		return "missing"
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func probeValidatorCapabilities(launch *validatorMetricsLaunch) error {
	raw, err := runValidatorMetricsCommand(launch, "metrics", "capabilities")
	if err != nil {
		return fmt.Errorf("capabilities_unavailable")
	}
	var response struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Protocol  int    `json:"protocol_version"`
		Version   int    `json:"capabilities_version"`
		Producer  struct {
			Name string `json:"name"`
		} `json:"producer"`
		Protocols    []int    `json:"protocol_versions"`
		Measurements []int    `json:"measurement_schema_versions"`
		Artifacts    []int    `json:"artifact_schema_versions"`
		Operations   []string `json:"operations"`
		Diagnostics  []string `json:"diagnostics"`
		Limits       struct {
			DefaultInventory int `json:"default_inventory_count"`
			MaxInventory     int `json:"maximum_inventory_count"`
			DefaultCount     int `json:"default_export_count"`
			MaxCount         int `json:"maximum_export_count"`
			DefaultBytes     int `json:"default_export_bytes"`
			MaxBytes         int `json:"maximum_export_bytes"`
			MaxRecord        int `json:"maximum_individual_record_bytes"`
		} `json:"limits"`
	}
	if protocolDecode(raw, &response) != nil || !response.OK || response.Version != 1 || response.Protocol != 1 || response.Operation != "capabilities" || response.Producer.Name != "agent-validator" || !slices.Contains(response.Protocols, 1) || !slices.Contains(response.Measurements, 1) {
		return fmt.Errorf("capabilities_unsupported")
	}
	for _, operation := range []string{"export", "acknowledge"} {
		if !slices.Contains(response.Operations, operation) {
			return fmt.Errorf("capabilities_unsupported")
		}
	}
	if response.Limits.MaxCount < 100 || response.Limits.MaxBytes < 1000000 {
		return fmt.Errorf("capabilities_limits_unsupported")
	}
	return nil
}
