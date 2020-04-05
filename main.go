/*
Copyright (c) 2017, UPMC Enterprises
All rights reserved.
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:
    * Redistributions of source code must retain the above copyright
      notice, this list of conditions and the following disclaimer.
    * Redistributions in binary form must reproduce the above copyright
      notice, this list of conditions and the following disclaimer in the
      documentation and/or other materials provided with the distribution.
    * Neither the name UPMC Enterprises nor the
      names of its contributors may be used to endorse or promote products
      derived from this software without specific prior written permission.
THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL UPMC ENTERPRISES BE LIABLE FOR ANY
DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
(INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
*/

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/registry-creds/providers"

	"github.com/flanksource/registry-creds/config"

	"github.com/Sirupsen/logrus"
	"github.com/cenkalti/backoff"
	"github.com/flanksource/registry-creds/k8sutil"
	flag "github.com/spf13/pflag"
	v1 "k8s.io/client-go/pkg/api/v1"
)

var (
	flags                    = flag.NewFlagSet("", flag.ContinueOnError)
	argKubecfgFile           = flags.String("kubecfg-file", "", `Location of kubecfg file for access to kubernetes master service; --kube_master_url overrides the URL part of this; if neither this nor --kube_master_url are provided, defaults to service account tokens`)
	argKubeMasterURL         = flags.String("kube-master-url", "", `URL to reach kubernetes master. Env variables in this flag will be expanded.`)
	argAWSSecretName         = flags.String("aws-secret-name", "awsecr-cred", `Default AWS secret name`)
	argDPRSecretName         = flags.String("dpr-secret-name", "dpr-secret", `Default Docker Private Registry secret name`)
	argGCRSecretName         = flags.String("gcr-secret-name", "gcr-secret", `Default GCR secret name`)
	argACRSecretName         = flags.String("acr-secret-name", "acr-secret", "Default Azure Container Registry secret name")
	argGCRURL                = flags.String("gcr-url", "https://gcr.io", `Default GCR URL`)
	argAWSRegion             = flags.String("aws-region", "us-east-1", `Default AWS region`)
	argDPRPassword           = flags.String("dpr-password", "", "Docker Private Registry password")
	argDPRServer             = flags.String("dpr-server", "", "Docker Private Registry server")
	argDPRUser               = flags.String("dpr-user", "", "Docker Private Registry user")
	argACRURL                = flags.String("acr-url", "", "Azure Container Registry URL")
	argACRClientID           = flags.String("acr-client-id", "", "Azure Container Registry client ID (user name)")
	argACRPassword           = flags.String("acr-password", "", "Azure Container Registry password (client secret)")
	argRefreshMinutes        = flags.Int("refresh-mins", 60, `Default time to wait before refreshing (60 minutes)`)
	argSkipKubeSystem        = flags.Bool("skip-kube-system", true, `If true, will not attempt to set ImagePullSecrets on the kube-system namespace`)
	argAWSAssumeRole         = flags.String("aws_assume_role", "", `If specified AWS will assume this role and use it to retrieve tokens`)
	argTokenGenFxnRetryType  = flags.String("token-retry-type", config.DefaultTokenGenRetryType, `The type of retry timer to use when generating a secret token; either simple or exponential (simple)`)
	argTokenGenFxnRetries    = flags.Int("token-retries", config.DefaultTokenGenRetries, `Default number of times to retry generating a secret token (3)`)
	argTokenGenFxnRetryDelay = flags.Int("token-retry-delay", config.DefaultTokenGenRetryDelay, `Default number of seconds to wait before retrying secret token generation (5 seconds)`)
)

var (
	awsAccountIDs []string

	// RetryCfg represents the currently-configured number of retries + retry delay
	RetryCfg RetryConfig

	// The retry backoff timers
	simpleBackoff      *backoff.ConstantBackOff
	exponentialBackoff *backoff.ExponentialBackOff
)

type dockerJSON struct {
	Auths map[string]registryAuth `json:"auths,omitempty"`
}

type registryAuth struct {
	Auth  string `json:"auth"`
	Email string `json:"email"`
}

type controller struct {
	k8sutil                *k8sutil.K8sutilInterface
	secretGeneratorBuilder SecretGeneratorBuilder
}

// RetryConfig represents the number of retries + the retry delay for retrying an operation if it should fail
type RetryConfig struct {
	Type                string
	NumberOfRetries     int
	RetryDelayInSeconds int
}

func generateSecretObj(tokens []providers.AuthToken, isJSONCfg bool, secretName string) (*v1.Secret, error) {
	secret := &v1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name: secretName,
		},
	}
	if isJSONCfg {
		auths := map[string]registryAuth{}
		for _, token := range tokens {
			auths[token.Endpoint] = registryAuth{
				Auth:  token.AccessToken,
				Email: "none",
			}
		}
		configJSON, err := json.Marshal(dockerJSON{Auths: auths})
		if err != nil {
			return secret, nil
		}
		secret.Data = map[string][]byte{".dockerconfigjson": configJSON}
		secret.Type = "kubernetes.io/dockerconfigjson"
	} else {
		if len(tokens) == 1 {
			secret.Data = map[string][]byte{
				".dockercfg": []byte(fmt.Sprintf(config.DockerCfgTemplate, tokens[0].Endpoint, tokens[0].AccessToken))}
			secret.Type = "kubernetes.io/dockercfg"
		}
	}
	return secret, nil
}

// SecretGenerator represents a token generation function for a registry service
type SecretGenerator struct {
	Name       string
	Provider   providers.Provider
	IsJSONCfg  bool
	SecretName string
}

type SecretGeneratorBuilder func() []SecretGenerator

func getSecretGenerators() []SecretGenerator {
	availableSecretGenerators := []SecretGenerator{
		{
			Name:       "gcr",
			Provider:   providers.NewGCR(*argGCRURL),
			IsJSONCfg:  false,
			SecretName: *argGCRSecretName,
		},
		{
			Name: "ecr",
			Provider: providers.NewECR(providers.ECRConfig{
				AwsAccountIds: awsAccountIDs,
				AWSAssumeRole: argAWSAssumeRole,
				AwsRegion:     *argAWSRegion,
			}),
			IsJSONCfg:  true,
			SecretName: *argAWSSecretName,
		},
		{
			Name: "dpr",
			Provider: providers.NewDPR(providers.DPRConfig{
				User:     *argDPRUser,
				Password: *argDPRPassword,
				Server:   *argDPRServer,
			}),
			IsJSONCfg:  true,
			SecretName: *argDPRSecretName,
		},
		{
			Name: "apr",
			Provider: providers.NewACR(providers.ACRConfig{
				RegistryURL: *argACRURL,
				ClientID:    *argACRClientID,
				Password:    *argACRPassword,
			}),
			IsJSONCfg:  true,
			SecretName: *argACRSecretName,
		},
	}

	secretGenerators := []SecretGenerator{}

	for _, s := range availableSecretGenerators {
		if s.Provider.Enabled() {
			secretGenerators = append(secretGenerators, s)
		} else {
			logrus.Errorf("Provider %s not enabled", s.Name)
		}
	}

	return secretGenerators
}

func (c *controller) processNamespace(namespace *v1.Namespace, secret *v1.Secret) error {
	log := logrus.WithField("function", "processNamespace")
	// Check if the secret exists for the namespace
	log.Debugf("checking for secret %s in namespace %s", secret.Name, namespace.GetName())
	_, err := c.k8sutil.GetSecret(namespace.GetName(), secret.Name)

	if err != nil {
		log.Debugf("Could not find secret %s in namespace %s; will try to create it", secret.Name, namespace.GetName())
		// Secret not found, create
		err := c.k8sutil.CreateSecret(namespace.GetName(), secret)
		if err != nil {
			return fmt.Errorf("could not create Secret: %v", err)
		}
		log.Infof("Created new secret %s in namespace %s", secret.Name, namespace.GetName())
	} else {
		// Existing secret needs updated
		log.Debugf("Found secret %s in namespace %s; will try to update it", secret.Name, namespace.GetName())
		err := c.k8sutil.UpdateSecret(namespace.GetName(), secret)
		if err != nil {
			return fmt.Errorf("could not update Secret: %v", err)
		}
		log.Infof("Updated secret %s in namespace %s", secret.Name, namespace.GetName())
	}

	// Check if ServiceAccount exists
	serviceAccount, err := c.k8sutil.GetServiceAccount(namespace.GetName(), "default")
	if err != nil {
		log.Errorf("error getting service account default in namespace %s: %s", namespace.GetName(), err)
		return fmt.Errorf("could not get ServiceAccounts: %v", err)
	}

	// Update existing one if image pull secrets already exists for aws ecr token
	imagePullSecretFound := false
	for i, imagePullSecret := range serviceAccount.ImagePullSecrets {
		if imagePullSecret.Name == secret.Name {
			serviceAccount.ImagePullSecrets[i] = v1.LocalObjectReference{Name: secret.Name}
			imagePullSecretFound = true
			break
		}
	}

	// Append to list of existing service accounts if there isn't one already
	if !imagePullSecretFound {
		serviceAccount.ImagePullSecrets = append(serviceAccount.ImagePullSecrets, v1.LocalObjectReference{Name: secret.Name})
	}

	log.Infof("Updating ServiceAccount %s in namespace %s", serviceAccount.Name, namespace.GetName())
	err = c.k8sutil.UpdateServiceAccount(namespace.GetName(), serviceAccount)
	if err != nil {
		log.Errorf("error updating ServiceAccount %s in namespace %s: %s", serviceAccount.Name, namespace.GetName(), err)
		return fmt.Errorf("could not update ServiceAccount: %v", err)
	}

	return nil
}

func (c *controller) generateSecrets() []*v1.Secret {
	var secrets []*v1.Secret
	secretGenerators := c.secretGeneratorBuilder()

	maxTries := RetryCfg.NumberOfRetries + 1
	for _, secretGenerator := range secretGenerators {
		resetRetryTimer()
		logrus.Infof("------------------ [%s] ------------------\n", secretGenerator.SecretName)

		var newTokens []providers.AuthToken
		tries := 0
		for {
			tries++
			logrus.Infof("Getting secret; try #%d of %d", tries, maxTries)
			tokens, err := secretGenerator.Provider.GetAuthToken()
			if err != nil {
				if tries < maxTries {
					delayDuration := nextRetryDuration()
					if delayDuration == backoff.Stop {
						logrus.Errorf("Error getting secret for provider %s. Retry timer exceeded max tries/duration; will not try again until the next refresh cycle. [Err: %s]", secretGenerator.SecretName, err)
						break
					}
					logrus.Errorf("Error getting secret for provider %s. Will try again after %f seconds. [Err: %s]", secretGenerator.SecretName, delayDuration.Seconds(), err)
					<-time.After(delayDuration)
					continue
				}
				logrus.Errorf("Error getting secret for provider %s. Tried %d time(s); will not try again until the next refresh cycle. [Err: %s]", secretGenerator.SecretName, tries, err)
				break
			} else {
				logrus.Infof("Successfully got secret for provider %s after trying %d time(s)", secretGenerator.SecretName, tries)
				newTokens = tokens
				break
			}
		}

		newSecret, err := generateSecretObj(newTokens, secretGenerator.IsJSONCfg, secretGenerator.SecretName)
		if err != nil {
			logrus.Errorf("Error generating secret for provider %s. Skipping secret provider until the next refresh cycle! [Err: %s]", secretGenerator.SecretName, err)
		} else {
			secrets = append(secrets, newSecret)
		}
	}
	return secrets
}

// SetupRetryTimer initializes and configures the Retry Timer
func SetupRetryTimer() {
	delayDuration := time.Duration(RetryCfg.RetryDelayInSeconds) * time.Second
	switch RetryCfg.Type {
	case config.RetryTypeSimple:
		simpleBackoff = backoff.NewConstantBackOff(delayDuration)
		break
	case config.RetryTypeExponential:
		exponentialBackoff = backoff.NewExponentialBackOff()
		break
	}
}

func resetRetryTimer() {
	switch RetryCfg.Type {
	case config.RetryTypeSimple:
		simpleBackoff.Reset()
		break
	case config.RetryTypeExponential:
		exponentialBackoff.Reset()
		break
	}
}

func nextRetryDuration() time.Duration {
	switch RetryCfg.Type {
	case config.RetryTypeSimple:
		return simpleBackoff.NextBackOff()
	case config.RetryTypeExponential:
		return exponentialBackoff.NextBackOff()
	default:
		return time.Duration(config.DefaultTokenGenRetryDelay) * time.Second
	}
}

func validateParams() {
	// Allow environment variables to overwrite args
	awsAccountIDEnv := os.Getenv("awsaccount")
	awsRegionEnv := os.Getenv("awsregion")
	argAWSAssumeRoleEnv := os.Getenv("aws_assume_role")
	dprPassword := os.Getenv(config.DockerPrivateRegistryPasswordKey)
	dprServer := os.Getenv(config.DockerPrivateRegistryServerKey)
	dprUser := os.Getenv(config.DockerPrivateRegistryUserKey)
	acrURL := os.Getenv(config.AcrURLKey)
	acrClientID := os.Getenv(config.AcrClientIDKey)
	acrPassword := os.Getenv(config.AcrPasswordKey)
	gcrURLEnv := os.Getenv("gcrurl")

	// initialize the retry configuration using command line values
	RetryCfg = RetryConfig{
		Type:                *argTokenGenFxnRetryType,
		NumberOfRetries:     *argTokenGenFxnRetries,
		RetryDelayInSeconds: *argTokenGenFxnRetryDelay,
	}
	// ensure command line values are valid
	if RetryCfg.Type != config.RetryTypeSimple && RetryCfg.Type != config.RetryTypeExponential {
		logrus.Errorf("Unknown Retry Timer type '%s'! Defaulting to %s", RetryCfg.Type, config.DefaultTokenGenRetryType)
		RetryCfg.Type = config.DefaultTokenGenRetryType
	}
	if RetryCfg.NumberOfRetries < 0 {
		logrus.Errorf("Cannot use a negative value for the number of retries! Defaulting to %d", config.DefaultTokenGenRetries)
		RetryCfg.NumberOfRetries = config.DefaultTokenGenRetries
	}
	if RetryCfg.RetryDelayInSeconds < 0 {
		logrus.Errorf("Cannot use a negative value for the retry delay in seconds! Defaulting to %d", config.DefaultTokenGenRetryDelay)
		RetryCfg.RetryDelayInSeconds = config.DefaultTokenGenRetryDelay
	}
	// look for overrides in environment variables and use them if they exist and are valid
	tokenType, ok := os.LookupEnv(config.TokenGenRetryTypeKey)
	if ok && len(tokenType) > 0 {
		if tokenType != config.RetryTypeSimple && tokenType != config.RetryTypeExponential {
			logrus.Errorf("Unknown Retry Timer type '%s'! Defaulting to %s", tokenType, config.DefaultTokenGenRetryType)
			RetryCfg.Type = config.DefaultTokenGenRetryType
		} else {
			RetryCfg.Type = tokenType
		}
	}
	tokenRetries, ok := os.LookupEnv(config.TokenGenRetriesKey)
	if ok && len(tokenRetries) > 0 {
		tokenRetriesInt, err := strconv.Atoi(tokenRetries)
		if err != nil {
			logrus.Errorf("Unable to parse value of environment variable %s! [Err: %s]", config.TokenGenRetriesKey, err)
		} else {
			if tokenRetriesInt < 0 {
				logrus.Errorf("Cannot use a negative value for environment variable %s! Defaulting to %d", config.TokenGenRetriesKey, config.DefaultTokenGenRetries)
				RetryCfg.NumberOfRetries = config.DefaultTokenGenRetries
			} else {
				RetryCfg.NumberOfRetries = tokenRetriesInt
			}
		}
	}
	tokenRetryDelay, ok := os.LookupEnv(config.TokenGenRetryDelayKey)
	if ok && len(tokenRetryDelay) > 0 {
		tokenRetryDelayInt, err := strconv.Atoi(tokenRetryDelay)
		if err != nil {
			logrus.Errorf("Unable to parse value of environment variable %s! [Err: %s]", config.TokenGenRetryDelayKey, err)
		} else {
			if tokenRetryDelayInt < 0 {
				logrus.Errorf("Cannot use a negative value for environment variable %s! Defaulting to %d", config.TokenGenRetryDelayKey, config.DefaultTokenGenRetryDelay)
				RetryCfg.RetryDelayInSeconds = config.DefaultTokenGenRetryDelay
			} else {
				RetryCfg.RetryDelayInSeconds = tokenRetryDelayInt
			}
		}
	}
	// Set up the Retry Timer
	SetupRetryTimer()

	if len(awsRegionEnv) > 0 {
		argAWSRegion = &awsRegionEnv
	}

	if len(awsAccountIDEnv) > 0 {
		awsAccountIDs = strings.Split(awsAccountIDEnv, ",")
	} else {
		awsAccountIDs = []string{""}
	}

	if len(dprPassword) > 0 {
		argDPRPassword = &dprPassword
	}

	if len(dprServer) > 0 {
		argDPRServer = &dprServer
	}

	if len(dprUser) > 0 {
		argDPRUser = &dprUser
	}

	if len(gcrURLEnv) > 0 {
		argGCRURL = &gcrURLEnv
	}

	if len(argAWSAssumeRoleEnv) > 0 {
		argAWSAssumeRole = &argAWSAssumeRoleEnv
	}

	if len(acrURL) > 0 {
		argACRURL = &acrURL
	}

	if len(acrClientID) > 0 {
		argACRClientID = &acrClientID
	}

	if len(acrPassword) > 0 {
		argACRPassword = &acrPassword
	}
}

func handler(c *controller, ns *v1.Namespace) error {
	logrus.Infof("Refreshing credentials for namespace %s", ns.GetName())
	secrets := c.generateSecrets()
	logrus.Infof("Got %d refreshed credentials for namespace %s", len(secrets), ns.GetName())
	for _, secret := range secrets {
		if *argSkipKubeSystem && ns.GetName() == "kube-system" {
			continue
		}

		logrus.Infof("Processing secret for namespace %s, secret %s", ns.Name, secret.Name)

		if err := c.processNamespace(ns, secret); err != nil {
			logrus.Errorf("error processing secret for namespace %s, secret %s: %s", ns.Name, secret.Name, err)
			return err
		}

		logrus.Infof("Finished processing secret for namespace %s, secret %s", ns.Name, secret.Name)
	}
	logrus.Infof("Finished refreshing credentials for namespace %s", ns.GetName())
	return nil
}

func main() {
	logrus.Info("Starting up...")
	err := flags.Parse(os.Args)
	if err != nil {
		logrus.Fatalf("Could not parse command line arguments! [Err: %s]", err)
	}

	validateParams()

	logrus.Info("Using AWS Account: ", strings.Join(awsAccountIDs, ","))
	logrus.Info("Using AWS Region: ", *argAWSRegion)
	logrus.Info("Using AWS Assume Role: ", *argAWSAssumeRole)
	logrus.Info("Refresh Interval (minutes): ", *argRefreshMinutes)
	logrus.Infof("Retry Timer: %s", RetryCfg.Type)
	logrus.Info("Token Generation Retries: ", RetryCfg.NumberOfRetries)
	logrus.Info("Token Generation Retry Delay (seconds): ", RetryCfg.RetryDelayInSeconds)

	util, err := k8sutil.New(*argKubecfgFile, *argKubeMasterURL)

	if err != nil {
		logrus.Error("Could not create k8s client!!", err)
	}

	c := &controller{util, getSecretGenerators}

	util.WatchNamespaces(time.Duration(*argRefreshMinutes)*time.Minute, func(ns *v1.Namespace) error {
		return handler(c, ns)
	})
}
