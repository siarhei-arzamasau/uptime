// Package openapi supplies the compatibility pass required by the pinned swaggo generator.
package openapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

// NormalizeNullable converts swaggo's x-nullable annotations into OpenAPI 3.1 type unions.
// It rewrites both generated files deterministically; read/decode/write errors are returned.
func NormalizeNullable(directory string) error {
	path := filepath.Join(directory, "swagger.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read generated contract: %w", err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("decode contract: %w", err)
	}
	normalize(document)
	data, err = json.MarshalIndent(document, "", "    ")
	if err != nil {
		return fmt.Errorf("encode contract: %w", err)
	}
	data = append(data, '\n')
	yamlData, err := yaml.JSONToYAML(data)
	if err != nil {
		return fmt.Errorf("encode YAML contract: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write JSON contract: %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "swagger.yaml"), yamlData, 0644); err != nil {
		return fmt.Errorf("write YAML contract: %w", err)
	}
	return nil
}

func normalize(value any) {
	switch value := value.(type) {
	case map[string]any:
		// The generator emits x-nullable but does not convert it to the JSON Schema null type.
		if value["x-nullable"] == true {
			if kind, ok := value["type"].(string); ok {
				value["type"] = []string{kind, "null"}
				delete(value, "x-nullable")
			}
		}
		for _, child := range value {
			normalize(child)
		}
	case []any:
		for _, child := range value {
			normalize(child)
		}
	}
}
