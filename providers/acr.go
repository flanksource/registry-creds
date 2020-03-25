package providers

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/flanksource/registry-creds/config"
)

type ACR struct {
	ACRConfig
}

type ACRConfig struct {
	RegistryURL string
	ClientID    string
	Password    string
}

func NewACR(config ACRConfig) Provider {
	acr := &ACR{ACRConfig: config}
	return acr
}

func (p *ACR) GetAuthToken() ([]AuthToken, error) {
	if p.RegistryURL == "" {
		return []AuthToken{}, fmt.Errorf("Azure Container Registry URL is missing; ensure %s parameter is set", config.AcrURLKey)
	}

	if p.ClientID == "" {
		return []AuthToken{}, fmt.Errorf("Client ID needed to access Azure Container Registry is missing; ensure %s parameter is set", config.AcrClientIDKey)
	}

	if p.Password == "" {
		return []AuthToken{}, fmt.Errorf("Password needed to access Azure Container Registry is missing; ensure %s paremeter is set", config.AcrClientIDKey)
	}

	token := base64.StdEncoding.EncodeToString([]byte(strings.Join([]string{p.ClientID, p.Password}, ":")))

	return []AuthToken{{AccessToken: token, Endpoint: p.RegistryURL}}, nil
}

func (p *ACR) Enabled() bool {
	return p.RegistryURL != "" && p.ClientID != "" && p.Password != ""
}
