package hubic

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ncw/swift"
)

type hubicAuthenticator struct {
	// HubicAuthorization is the basicAuth header used
	// within requests to Hubic OAUTH2 API.
	HubicAuthorization string

	// HubicRefreshToken is the OAUTH2 refresh token.
	HubicRefreshToken string

	transport  http.RoundTripper
	storageURL string
	token      string
}

var _ swift.Authenticator = &hubicAuthenticator{}

const (
	// HubicEndpoint is the HubiC API URL
	HubicEndpoint = "https://api.hubic.com"
)

// oauth token info as per https://tools.ietf.org/html/rfc6749#section-4.2.2
type hubicToken struct {

	// The access token issued by the authorization server.
	AccessToken string `json:"access_token"`

	// The type of the token issued
	TokenType string `json:"token_type"`

	// The lifetime in seconds of the access token
	ExpiresIn int `json:"expires_in"`
}

// HubiC credentials to connect to file API as per https://api.hubic.com/console/#/account/credentials
type hubicCredentials struct {
	// Openstack endpoint
	Endpoint string `json:"endpoint"`

	// Openstack token
	Token string `json:"token"`

	// Expires date, e.g. "2017-12-25T13:51:31+01:00"
	Expires time.Time `json:"expires"`
}

// Request creates an http.Request for the auth - return nil if not needed
func (v *hubicAuthenticator) Request(c *swift.Connection) (*http.Request, error) {
	// hubic requires two requests to do authentication
	// 1. POST /oauth/token to get oauth token required to access credentials API
	// 2. GET /1.0/account/credentials to get Swift storageURL and token
	//
	// swift.Authenticator does not support two-request authentication, so we do
	// all work here and return nil to indicate no additional requests are needed

	// The code below is mostly copy&paste from https://github.com/ovh/svfs/blob/v0.9.1/svfs/hubic.go

	// TODO honour connection timeout configuration

	// Request new oauth token
	form := url.Values{}
	form.Add("refresh_token", v.HubicRefreshToken)
	form.Add("grant_type", "refresh_token")
	req, err := http.NewRequest("POST", HubicEndpoint+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	// req.Header.Add("User-Agent", swift.DefaultUserAgent)
	req.Header.Add("Authorization", "Basic "+v.HubicAuthorization)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	apiResp, err := v.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer apiResp.Body.Close()
	if apiResp.StatusCode != 200 {
		return nil, fmt.Errorf("Invalid reply from server when fetching hubic API token : %s", apiResp.Status)
	}
	body, err := ioutil.ReadAll(apiResp.Body)
	if err != nil {
		return nil, err
	}
	var apiToken hubicToken
	if err := json.Unmarshal(body, &apiToken); err != nil {
		return nil, err
	}

	// Request new keystone token
	req, err = http.NewRequest("GET", HubicEndpoint+"/1.0/account/credentials", nil)
	if err != nil {
		return nil, err
	}
	// req.Header.Add("User-Agent", swift.DefaultUserAgent)
	req.Header.Add("Authorization", apiToken.TokenType+" "+apiToken.AccessToken)
	resp, err := v.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Invalid reply from server when fetching hubic credentials : %s", resp.Status)
	}
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var credentials hubicCredentials
	if err := json.Unmarshal(body, &credentials); err != nil {
		return nil, err
	}

	v.storageURL = credentials.Endpoint
	v.token = credentials.Token

	return nil, nil
}

// Response parses the http.Response
func (v *hubicAuthenticator) Response(resp *http.Response) error {
	return nil
}

// The public storage URL - set Internal to true to read
// internal/service net URL
func (v *hubicAuthenticator) StorageUrl(Internal bool) string {
	return v.storageURL
}

// The access token
func (v *hubicAuthenticator) Token() string {
	return v.token
}

// The CDN url if available
func (v *hubicAuthenticator) CdnUrl() string {
	return ""
}
