package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// apiGatewayURL is Atlassian's API gateway, which bearer tokens (service account and
// scoped API tokens, OAuth) must go through instead of the site URL. A var so tests can
// point it at a fake server.
var apiGatewayURL = "https://api.atlassian.com"

// Client is a minimal Jira Cloud REST client.
type Client struct {
	siteURL   string // the Jira site, for browse links
	apiURL    string // where REST requests go: the site, or the gateway for bearer tokens
	authorize func(*http.Request)
	http      *http.Client
}

// NewClient returns a Client that signs requests with basic auth (email + classic API
// token) and sends them to siteURL, e.g. https://your-site.atlassian.net.
func NewClient(siteURL, email, token string, timeout time.Duration) *Client {
	site := trimURL(siteURL)
	return &Client{
		siteURL:   site,
		apiURL:    site,
		authorize: func(r *http.Request) { r.SetBasicAuth(email, token) },
		http:      &http.Client{Timeout: timeout},
	}
}

// NewBearerClient returns a Client that signs requests with a bearer token and sends
// them through the API gateway for the site with the given cloud ID. siteURL is only
// used for browse links, which the gateway doesn't serve.
func NewBearerClient(siteURL, cloudID, token string, timeout time.Duration) *Client {
	return &Client{
		siteURL:   trimURL(siteURL),
		apiURL:    trimURL(apiGatewayURL) + "/ex/jira/" + url.PathEscape(cloudID),
		authorize: func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) },
		http:      &http.Client{Timeout: timeout},
	}
}

// LookupCloudID returns the cloud ID of the Jira site at siteURL, which bearer requests
// need. It reads the site's public tenant info, so needs no credentials.
func LookupCloudID(siteURL string, timeout time.Duration) (string, error) {
	const path = "/_edge/tenant_info"
	resp, err := (&http.Client{Timeout: timeout}).Get(trimURL(siteURL) + path)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s returned %d", path, resp.StatusCode)
	}
	var info struct {
		CloudID string `json:"cloudId"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&info); err != nil {
		return "", fmt.Errorf("GET %s: unexpected response: %w", path, err)
	}
	if info.CloudID == "" {
		return "", fmt.Errorf("GET %s: no cloudId in the response", path)
	}
	return info.CloudID, nil
}

func trimURL(u string) string {
	return strings.TrimRight(strings.TrimSpace(u), "/")
}

// APIError is a non-2xx response from Jira.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("%s %s returned %d", e.Method, e.Path, e.StatusCode)
	switch e.StatusCode {
	case http.StatusUnauthorized:
		msg += " (check the jira_api_token input, plus jira_user_email for basic auth or the token's scopes for bearer)"
	case http.StatusForbidden, http.StatusNotFound:
		msg += " (the ticket doesn't exist, or the Jira user can't see it)"
	}
	if body := strings.TrimSpace(e.Body); body != "" {
		msg += ": " + body
	}
	return msg
}

// Transition is one way a ticket can move from its current status.
type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   struct {
		Name string `json:"name"`
	} `json:"to"`
}

// IssueURL returns the browser link for key.
func (c *Client) IssueURL(key string) string {
	return c.siteURL + "/browse/" + key
}

// GetStatus returns the name of the ticket's current status.
func (c *Client) GetStatus(key string) (string, error) {
	var resp struct {
		Fields struct {
			Status struct {
				Name string `json:"name"`
			} `json:"status"`
		} `json:"fields"`
	}
	if err := c.do(http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"?fields=status", nil, &resp); err != nil {
		return "", err
	}
	return resp.Fields.Status.Name, nil
}

// AddComment posts body, in Jira wiki markup, as a comment on the ticket.
// It uses API v2 because v3 only accepts Atlassian Document Format.
func (c *Client) AddComment(key, body string) error {
	return c.do(http.MethodPost, "/rest/api/2/issue/"+url.PathEscape(key)+"/comment", map[string]string{"body": body}, nil)
}

// Transitions lists the transitions available from the ticket's current status.
func (c *Client) Transitions(key string) ([]Transition, error) {
	var resp struct {
		Transitions []Transition `json:"transitions"`
	}
	if err := c.do(http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"/transitions", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Transitions, nil
}

// Transition moves the ticket using the transition with the given ID.
func (c *Client) Transition(key, id string) error {
	payload := map[string]any{"transition": map[string]string{"id": id}}
	return c.do(http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(key)+"/transitions", payload, nil)
}

func (c *Client) do(method, path string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.apiURL+path, body)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	c.authorize(req)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// url.Error includes the request URL, which never contains credentials.
		var urlErr *url.Error
		if errors.As(err, &urlErr) && urlErr.Timeout() {
			return fmt.Errorf("%s %s: Jira didn't respond within %s", method, path, c.http.Timeout)
		}
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("%s %s: reading response: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("%s %s: unexpected response: %w", method, path, err)
		}
	}
	return nil
}

// FindTransition returns the transition whose destination status matches target,
// ignoring case. It matches on the destination rather than the transition's own
// name, because the two can differ.
func FindTransition(transitions []Transition, target string) (Transition, error) {
	target = strings.TrimSpace(target)
	for _, t := range transitions {
		if strings.EqualFold(t.To.Name, target) {
			return t, nil
		}
	}

	var reachable []string
	for _, t := range transitions {
		reachable = append(reachable, fmt.Sprintf("%q", t.To.Name))
	}
	list := "none"
	if len(reachable) > 0 {
		list = strings.Join(reachable, ", ")
	}
	return Transition{}, fmt.Errorf("status %q can't be reached from the ticket's current status; reachable statuses: %s", target, list)
}
