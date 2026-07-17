package authn

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/chaos-plus/chaosplus/internal/core/extension/secretx"
)

const RuntimeResourcesVersion = 1

type RuntimeResources struct {
	Version   int    `json:"version"`
	ProjectID string `json:"project_id"`
	ClientID  string `json:"client_id"`
}

func LoadRuntimeResources(path string) (RuntimeResources, error) {
	f, err := os.Open(path)
	if err != nil {
		return RuntimeResources{}, fmt.Errorf("open authn resources: %w", err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	var resources RuntimeResources
	if err := decoder.Decode(&resources); err != nil {
		return RuntimeResources{}, fmt.Errorf("decode authn resources: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return RuntimeResources{}, fmt.Errorf("decode authn resources: trailing JSON value")
	}
	if resources.Version != RuntimeResourcesVersion || resources.ProjectID == "" || resources.ClientID == "" {
		return RuntimeResources{}, fmt.Errorf("invalid authn resources")
	}
	return resources, nil
}

func WriteRuntimeResources(path string, resources RuntimeResources) error {
	if path == "" {
		return fmt.Errorf("authn resources output path is required")
	}
	if resources.Version != RuntimeResourcesVersion || resources.ProjectID == "" || resources.ClientID == "" {
		return fmt.Errorf("invalid authn resources")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create authn resources directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".authn-resources-*")
	if err != nil {
		return fmt.Errorf("create authn resources temp file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o640); err != nil {
		temp.Close()
		return fmt.Errorf("chmod authn resources: %w", err)
	}
	encoder := json.NewEncoder(temp)
	if err := encoder.Encode(resources); err != nil {
		temp.Close()
		return fmt.Errorf("encode authn resources: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync authn resources: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close authn resources: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("publish authn resources: %w", err)
	}
	return nil
}

func ResolveConfig(cfg Config) (Config, error) {
	key, err := secretx.Resolve("authn.web.encryption_key", cfg.Web.EncryptionKey, cfg.Web.EncryptionKeyFile, 4096)
	if err != nil {
		return Config{}, err
	}
	cfg.Web.EncryptionKey = key
	if cfg.ResourcesFile == "" {
		return cfg, nil
	}
	resources, err := LoadRuntimeResources(cfg.ResourcesFile)
	if err != nil {
		return Config{}, err
	}
	if len(cfg.Audience) > 0 && (len(cfg.Audience) != 1 || cfg.Audience[0] != resources.ProjectID) {
		return Config{}, fmt.Errorf("authn audience conflicts with resources_file")
	}
	if cfg.Web.ClientID != "" && cfg.Web.ClientID != resources.ClientID {
		return Config{}, fmt.Errorf("authn web client_id conflicts with resources_file")
	}
	cfg.Audience = []string{resources.ProjectID}
	cfg.Web.ClientID = resources.ClientID
	return cfg, nil
}
