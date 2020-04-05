package providers

import (
	"github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
)

type ECR struct {
	ECRConfig
	ecrClient *ecr.ECR
}

type ECRConfig struct {
	AwsAccountIds []string
	AwsRegion     string
	AWSAssumeRole *string
}

func NewECR(config ECRConfig) Provider {
	client := newEcrClient(config)
	ecr := &ECR{
		ECRConfig: config,
		ecrClient: client,
	}
	return ecr
}

func (p *ECR) Enabled() bool {
	return p.AwsRegion != "" && len(p.AwsAccountIds) > 0
}

func (p *ECR) GetAuthToken() ([]AuthToken, error) {
	var tokens []AuthToken
	var regIds []*string
	regIds = make([]*string, len(p.AwsAccountIds))

	for i, awsAccountID := range p.AwsAccountIds {
		regIds[i] = aws.String(awsAccountID)
	}

	params := &ecr.GetAuthorizationTokenInput{
		RegistryIds: regIds,
	}

	resp, err := p.ecrClient.GetAuthorizationToken(params)

	if err != nil {
		// Print the error, cast err to awserr.Error to get the Code and
		// Message from an error.
		logrus.Println(err.Error())
		return []AuthToken{}, err
	}

	for _, auth := range resp.AuthorizationData {
		tokens = append(tokens, AuthToken{
			AccessToken: *auth.AuthorizationToken,
			Endpoint:    *auth.ProxyEndpoint,
		})

	}
	return tokens, nil
}

func newEcrClient(config ECRConfig) *ecr.ECR {
	sess := session.Must(session.NewSession())
	awsConfig := aws.NewConfig().WithRegion(config.AwsRegion)

	if *config.AWSAssumeRole != "" {
		creds := stscreds.NewCredentials(sess, *config.AWSAssumeRole)
		awsConfig.Credentials = creds
	}

	return ecr.New(sess, awsConfig)
}
