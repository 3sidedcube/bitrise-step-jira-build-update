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

// Client is a minimal Jira Cloud REST client using basic auth (email + API token).
type Client struct {
	baseURL string
	email   string
	token   string
	http    *http.Client
}

// NewClient returns a Client for baseURL, e.g. https://your-site.atlassian.net.
func NewClient(baseURL, email, token string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		email:   email,
		token:   token,
		http:    &http.Client{Timeout: timeout},
	}
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
		msg += " (check the jira_user_email and jira_api_token inputs)"
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
	return c.baseURL + "/browse/" + key
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

	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	req.SetBasicAuth(c.email, c.token)
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
