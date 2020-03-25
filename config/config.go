package config

const (
	// Retry Types
	RetryTypeSimple      = "simple"
	RetryTypeExponential = "exponential"

	DockerCfgTemplate                = `{"%s":{"username":"oauth2accesstoken","password":"%s","email":"none"}}`
	DockerPrivateRegistryPasswordKey = "DOCKER_PRIVATE_REGISTRY_PASSWORD"
	DockerPrivateRegistryServerKey   = "DOCKER_PRIVATE_REGISTRY_SERVER"
	DockerPrivateRegistryUserKey     = "DOCKER_PRIVATE_REGISTRY_USER"
	AcrURLKey                        = "ACR_URL"
	AcrClientIDKey                   = "ACR_CLIENT_ID"
	AcrPasswordKey                   = "ACR_PASSWORD"
	TokenGenRetryTypeKey             = "TOKEN_RETRY_TYPE"
	TokenGenRetriesKey               = "TOKEN_RETRIES"
	TokenGenRetryDelayKey            = "TOKEN_RETRY_DELAY"
	DefaultTokenGenRetries           = 3
	DefaultTokenGenRetryDelay        = 5 // in seconds
	DefaultTokenGenRetryType         = RetryTypeSimple
)
