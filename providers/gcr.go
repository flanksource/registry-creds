package providers

import (
	"context"
	"fmt"
	"os/user"
	"path"
	"os"

	log "github.com/Sirupsen/logrus"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type GCR struct {
	gcrClient GCRInterface
	url       string
}

func NewGCR(url string) Provider {
	client := newGcrClient()
	gcr := &GCR{
		gcrClient: client,
		url:       url,
	}
	return gcr
}

func (p *GCR) GetAuthToken() ([]AuthToken, error) {
	ts, err := p.gcrClient.DefaultTokenSource(context.TODO(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return []AuthToken{}, err
	}

	token, err := ts.Token()
	if err != nil {
		return []AuthToken{}, err
	}

	if !token.Valid() {
		return []AuthToken{}, fmt.Errorf("token was invalid")
	}

	if token.Type() != "Bearer" {
		return []AuthToken{}, fmt.Errorf(fmt.Sprintf("expected token type \"Bearer\" but got \"%s\"", token.Type()))
	}

	tokens := make([]AuthToken, 0)
	tokens = append(tokens, AuthToken{token.AccessToken, p.url})

	return tokens, nil
}

func (p *GCR) Enabled() bool {
	usr, err := user.Current()
	if err != nil {
		log.Errorf("Failed to get current user: %v", err)
		return false
	}
	filename := path.Join(usr.HomeDir, ".config", "gcloud", "application_default_credentials.json")

	info, err := os.Stat(filename)
	if err != nil && os.IsNotExist(err) {
		return false
	} else if err != nil {
		log.Errorf("Failed to list %s: %v", filename, err)
		return false
	} else if info.IsDir() {
		log.Errorf("File %s is a directory")
		return false
	}

	return p.url != ""
}

type GCRInterface interface {
	DefaultTokenSource(ctx context.Context, scope ...string) (oauth2.TokenSource, error)
}

type GCRClient struct{}

func (gcr GCRClient) DefaultTokenSource(ctx context.Context, scope ...string) (oauth2.TokenSource, error) {
	return google.DefaultTokenSource(ctx, scope...)
}

func newGcrClient() GCRInterface {
	return GCRClient{}
}
