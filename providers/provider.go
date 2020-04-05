package providers

// AuthToken represents an Access Token and an Endpoint for a registry service
type AuthToken struct {
	AccessToken string
	Endpoint    string
}

type Provider interface {
	GetAuthToken() ([]AuthToken, error)
	Enabled() bool
}
