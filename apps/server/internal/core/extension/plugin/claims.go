// Package plugin loads capability-limited WebAssembly extensions.
package plugin

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/pkg/interpreter"
)

const (
	claimsABI      = 1
	claimsHook     = "claims"
	defaultTimeout = 50 * time.Millisecond
	maxTimeout     = time.Second
	defaultMemory  = 256 // 16MiB
	maxMemory      = 1024
	defaultOutput  = 64 * 1024
	maxModuleBytes = 16 * 1024 * 1024
	failureDeny    = "deny"
	failureIgnore  = "ignore"
)

var claimName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type Config struct {
	Enabled       bool              `mapstructure:"enabled" description:"enable signed WASM claim plugins" default:"false"`
	Manifests     []string          `mapstructure:"manifests" description:"JSON plugin manifest paths"`
	TrustedKeys   map[string]string `mapstructure:"trusted_keys" description:"Ed25519 public keys by key id, base64 encoded"`
	AllowUnsigned bool              `mapstructure:"allow_unsigned" description:"allow unsigned manifests; development only" default:"false"`
}

// Manifest is signed metadata for one claim-enrichment module. Signatures cover
// the identity, ABI, hook and module digest, not filesystem paths.
type Manifest struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	ABI            int    `json:"abi"`
	Hook           string `json:"hook"`
	Module         string `json:"module"`
	SHA256         string `json:"sha256"`
	KeyID          string `json:"key_id,omitempty"`
	Signature      string `json:"signature,omitempty"`
	FailureMode    string `json:"failure_mode,omitempty"`
	TimeoutMS      int    `json:"timeout_ms,omitempty"`
	MemoryPages    uint32 `json:"memory_pages,omitempty"`
	MaxOutputBytes uint32 `json:"max_output_bytes,omitempty"`
}

type claimPlugin struct {
	manifest Manifest
	runtime  interpreter.ByteRuntime
}

type Claims struct {
	plugins []*claimPlugin
}

func LoadClaims(config Config) (*Claims, error) {
	if !config.Enabled {
		return nil, nil
	}
	if len(config.Manifests) == 0 {
		return nil, errors.New("WASM plugins are enabled but no manifests are configured")
	}
	chain := &Claims{}
	for _, path := range config.Manifests {
		loaded, err := loadClaimPlugin(path, config)
		if err != nil {
			_ = chain.Close()
			return nil, err
		}
		chain.plugins = append(chain.plugins, loaded)
	}
	return chain, nil
}

func loadClaimPlugin(path string, config Config) (*claimPlugin, error) {
	manifestData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin manifest %q: %w", path, err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestData)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode plugin manifest %q: %w", path, err)
	}
	if err := validateManifest(manifest, config); err != nil {
		return nil, fmt.Errorf("validate plugin manifest %q: %w", path, err)
	}
	modulePath, err := confinedModulePath(filepath.Dir(path), manifest.Module)
	if err != nil {
		return nil, fmt.Errorf("resolve plugin %q module: %w", manifest.Name, err)
	}
	module, err := os.ReadFile(modulePath)
	if err != nil {
		return nil, fmt.Errorf("read plugin %q module: %w", manifest.Name, err)
	}
	if len(module) > maxModuleBytes {
		return nil, fmt.Errorf("plugin %q module exceeds %d bytes", manifest.Name, maxModuleBytes)
	}
	digest := sha256.Sum256(module)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), manifest.SHA256) {
		return nil, fmt.Errorf("plugin %q module digest mismatch", manifest.Name)
	}
	memoryPages := manifest.MemoryPages
	if memoryPages == 0 {
		memoryPages = defaultMemory
	}
	runtime, err := interpreter.New(interpreter.EngineWasm, interpreter.WithWASM(module), interpreter.WithWASMMemoryLimitPages(memoryPages))
	if err != nil {
		return nil, fmt.Errorf("instantiate plugin %q: %w", manifest.Name, err)
	}
	return &claimPlugin{manifest: manifest, runtime: runtime.(interpreter.ByteRuntime)}, nil
}

func validateManifest(manifest Manifest, config Config) error {
	if !claimName.MatchString(manifest.Name) || strings.TrimSpace(manifest.Version) == "" || manifest.ABI != claimsABI || manifest.Hook != claimsHook {
		return errors.New("invalid name, version, ABI, or hook")
	}
	if manifest.Module == "" || len(manifest.SHA256) != sha256.Size*2 {
		return errors.New("module and SHA-256 digest are required")
	}
	if manifest.FailureMode == "" {
		manifest.FailureMode = failureDeny
	}
	if manifest.FailureMode != failureDeny && manifest.FailureMode != failureIgnore {
		return errors.New("failure_mode must be deny or ignore")
	}
	if manifest.TimeoutMS < 0 || time.Duration(manifest.TimeoutMS)*time.Millisecond > maxTimeout {
		return fmt.Errorf("timeout_ms must be between 0 and %d", maxTimeout.Milliseconds())
	}
	if manifest.MemoryPages > maxMemory {
		return fmt.Errorf("memory_pages must not exceed %d", maxMemory)
	}
	if manifest.MaxOutputBytes > defaultOutput {
		return fmt.Errorf("max_output_bytes must not exceed %d", defaultOutput)
	}
	if manifest.Signature == "" {
		if config.AllowUnsigned {
			return nil
		}
		return errors.New("unsigned plugin rejected")
	}
	encodedKey, ok := config.TrustedKeys[manifest.KeyID]
	if !ok {
		return fmt.Errorf("untrusted signing key %q", manifest.KeyID)
	}
	key, err := decodeBase64(encodedKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("invalid trusted Ed25519 public key")
	}
	signature, err := decodeBase64(manifest.Signature)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(key), manifestPayload(manifest), signature) {
		return errors.New("invalid manifest signature")
	}
	return nil
}

func (c *Claims) Enrich(ctx context.Context, input authnmod.ClaimContext) (map[string]any, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode claim plugin input: %w", err)
	}
	result := make(map[string]any, len(c.plugins))
	for _, plugin := range c.plugins {
		timeout := defaultTimeout
		if plugin.manifest.TimeoutMS > 0 {
			timeout = time.Duration(plugin.manifest.TimeoutMS) * time.Millisecond
		}
		maxOutput := uint32(defaultOutput)
		if plugin.manifest.MaxOutputBytes > 0 {
			maxOutput = plugin.manifest.MaxOutputBytes
		}
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		output, callErr := plugin.runtime.CallBytes(callCtx, claimsHook, encoded, maxOutput)
		cancel()
		if callErr == nil {
			var claims map[string]any
			callErr = json.Unmarshal(output, &claims)
			if callErr == nil {
				callErr = validateClaims(claims)
			}
			if callErr == nil && len(claims) > 0 {
				result[plugin.manifest.Name] = claims
			}
		}
		if callErr != nil {
			if plugin.failureMode() == failureIgnore {
				slog.Warn("claim plugin failed; output ignored", "plugin", plugin.manifest.Name, "err", callErr)
				continue
			}
			return nil, fmt.Errorf("claim plugin %q: %w", plugin.manifest.Name, callErr)
		}
	}
	return result, nil
}

func (c *Claims) Stop(context.Context) error { return c.Close() }

func (c *Claims) Close() error {
	var errs []error
	for _, plugin := range c.plugins {
		errs = append(errs, plugin.runtime.Close())
	}
	return errors.Join(errs...)
}

func (p *claimPlugin) failureMode() string {
	if p.manifest.FailureMode == "" {
		return failureDeny
	}
	return p.manifest.FailureMode
}

func validateClaims(claims map[string]any) error {
	if len(claims) > 32 {
		return errors.New("plugin returned more than 32 claims")
	}
	for key, value := range claims {
		if !claimName.MatchString(key) || !claimValue(value) {
			return fmt.Errorf("unsupported claim %q", key)
		}
	}
	return nil
}

func claimValue(value any) bool {
	switch typed := value.(type) {
	case nil, bool, string, float64:
		return true
	case []any:
		if len(typed) > 64 {
			return false
		}
		for _, item := range typed {
			if !claimValue(item) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func confinedModulePath(base, module string) (string, error) {
	if filepath.IsAbs(module) {
		return "", errors.New("module path must be relative to its manifest")
	}
	base, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.Abs(filepath.Join(base, module))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(base, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("module path escapes the manifest directory")
	}
	return resolved, nil
}

func manifestPayload(manifest Manifest) []byte {
	return []byte(strings.Join([]string{manifest.Name, manifest.Version, strconv.Itoa(manifest.ABI), manifest.Hook, strings.ToLower(manifest.SHA256)}, "\n"))
}

func decodeBase64(value string) ([]byte, error) {
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.StdEncoding.DecodeString(value)
}
