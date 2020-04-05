package providers

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/flanksource/registry-creds/config"
)

type DPR struct {
	DPRConfig
}

type DPRConfig struct {
	User     string
	Password string
	Server   string
}

func NewDPR(config DPRConfig) Provider {
	dpr := &DPR{DPRConfig: config}
	return dpr
}

func (p *DPR) GetAuthToken() ([]AuthToken, error) {
	if p.Server == "" {
		return []AuthToken{}, fmt.Errorf(fmt.Sprintf("Failed to get auth token for docker private registry: empty value for %s", config.DockerPrivateRegistryServerKey))
	}

	if p.User == "" {
		return []AuthToken{}, fmt.Errorf(fmt.Sprintf("Failed to get auth token for docker private registry: empty value for %s", config.DockerPrivateRegistryUserKey))
	}

	if p.Password == "" {
		return []AuthToken{}, fmt.Errorf(fmt.Sprintf("Failed to get auth token for docker private registry: empty value for %s", config.DockerPrivateRegistryPasswordKey))
	}

	token := base64.StdEncoding.EncodeToString([]byte(strings.Join([]string{p.User, p.Password}, ":")))

	return []AuthToken{{AccessToken: token, Endpoint: p.Server}}, nil
}

func (p *DPR) Enabled() bool {
	return p.Server != "" && p.User != "" && p.Password != ""
}
