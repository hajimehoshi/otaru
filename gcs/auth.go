package gcs

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/cloud/storage"
)

const (
	storageScope = storage.ScopeFullControl
)

func GetGCloudTokenViaWebUI(conf *oauth2.Config) (*oauth2.Token, error) {
	authurl := conf.AuthCodeURL("fixmeee", oauth2.AccessTypeOffline)
	fmt.Printf("visit %v\n", authurl)
	fmt.Printf("paste code:")

	var code string
	if _, err := fmt.Scan(&code); err != nil {
		return nil, fmt.Errorf("Failed to scan auth code: %v", err)
	}
	initialToken, err := conf.Exchange(oauth2.NoContext, code)
	if err != nil {
		log.Fatalf("Failed to use auth code: %v", err)
	}

	return initialToken, nil
}

func getGCloudTokenCached(tokenCacheFilePath string) (*oauth2.Token, error) {
	tokenJson, err := ioutil.ReadFile(tokenCacheFilePath)
	if err != nil {
		return nil, fmt.Errorf("Failed to read token cache file: %v", tokenCacheFilePath)
	}

	var token oauth2.Token
	if err = json.Unmarshal(tokenJson, &token); err != nil {
		return nil, fmt.Errorf("Failed to parse token cache file: %v", tokenCacheFilePath)
	}

	if token.Expiry.Before(time.Now()) {
		return nil, fmt.Errorf("Cached token already expired: %v", token.Expiry)
	}

	return &token, nil
}

func updateGCloudTokenCache(token *oauth2.Token, tokenCacheFilePath string) {
	tjson, err := json.Marshal(token)
	if err != nil {
		log.Fatalf("Serializing token failed: %v", err)
	}

	if err = ioutil.WriteFile(tokenCacheFilePath, tjson, 0600); err != nil {
		log.Printf("Writing token cache failed: %v", err)
	}
}

type ClientSource func(ctx context.Context) *http.Client

func GetGCloudClientSource(credentialsFilePath string, tokenCacheFilePath string, tryWebUI bool) (ClientSource, error) {
	credentialsJson, err := ioutil.ReadFile(credentialsFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read google cloud client-secret file: %v", err)
	}

	conf, err := google.ConfigFromJSON(credentialsJson, storageScope)
	if err != nil {
		return nil, fmt.Errorf("invalid google cloud key json: %v", err)
	}

	var initialToken *oauth2.Token
	if initialToken, err = getGCloudTokenCached(tokenCacheFilePath); err != nil {
		if !tryWebUI {
			return nil, fmt.Errorf("OAuth2 token cache invalid.")
		}

		if initialToken, err = GetGCloudTokenViaWebUI(conf); err != nil {
			return nil, fmt.Errorf("Failed to get valid gcloud token: %v", err)
		}
		updateGCloudTokenCache(initialToken, tokenCacheFilePath)
	}

	return func(ctx context.Context) *http.Client { return conf.Client(ctx, initialToken) }, nil
}
