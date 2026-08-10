package channelaccount

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/stellar/go-stellar-sdk/keypair"
)

// SecretSource supplies channel account secret seeds. Implementations are the
// pluggable boundary between the service and wherever secrets actually live:
// an environment variable for local dev, a real secrets manager in production.
type SecretSource interface {
	ChannelAccountSecrets(ctx context.Context) ([]string, error)
}

// EnvSecretSource reads a comma-separated list of channel account secret seeds
// from a single environment variable. Intended for local development only —
// production deployments should back SecretSource with a real secrets manager.
type EnvSecretSource struct {
	VarName string
}

// NewEnvSecretSource builds an EnvSecretSource for the named environment variable.
func NewEnvSecretSource(varName string) *EnvSecretSource {
	return &EnvSecretSource{VarName: varName}
}

// ChannelAccountSecrets parses and validates the seeds. It never logs or echoes
// secret values; validation errors reference only the position in the list.
func (s *EnvSecretSource) ChannelAccountSecrets(ctx context.Context) ([]string, error) {
	raw := os.Getenv(s.VarName)
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%s is not set: at least one channel account secret is required", s.VarName)
	}

	parts := strings.Split(raw, ",")
	secrets := make([]string, 0, len(parts))
	for _, p := range parts {
		seed := strings.TrimSpace(p)
		if seed == "" {
			continue
		}
		if _, err := keypair.ParseFull(seed); err != nil {
			return nil, fmt.Errorf("invalid channel account secret at position %d: %w", len(secrets), err)
		}
		secrets = append(secrets, seed)
	}

	if len(secrets) == 0 {
		return nil, fmt.Errorf("%s contained no valid channel account secrets", s.VarName)
	}
	return secrets, nil
}
