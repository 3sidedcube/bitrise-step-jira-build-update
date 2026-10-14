package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func transitions(to ...string) []Transition {
	var ts []Transition
	for i, name := range to {
		t := Transition{ID: string(rune('1' + i)), Name: "Go to " + name}
		t.To.Name = name
		ts = append(ts, t)
	}
	return ts
}

func TestFindTransition(t *testing.T) {
	ts := transitions("In Progress", "In Review", "Done")

	got, err := FindTransition(ts, " in review ")
	if err != nil || got.ID != "2" {
		t.Fatalf("got (%+v, %v), want transition 2", got, err)
	}

	// Matches on the destination, not the transition's name.
	if _, err := FindTransition(ts, "Go to Done"); err == nil {
		t.Error("matched on the transition name")
	}

	_, err = FindTransition(ts, "QA")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{`"QA"`, `"In Progress", "In Review", "Done"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q doesn't contain %s", err, want)
		}
	}

	if _, err := FindTransition(nil, "QA"); err == nil || !strings.Contains(err.Error(), "none") {
		t.Errorf("got %v, want an error listing no statuses", err)
	}
}

type recorded struct {
	method, path, query, body, user, pass, contentType string
}

// fakeJira serves handler and records every request it receives.
func fakeJira(t *testing.T, handler http.HandlerFunc) (*Client, *[]recorded) {
	t.Helper()
	var reqs []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		user, pass, _ := r.BasicAuth()
		reqs = append(reqs, recorded{r.Method, r.URL.Path, r.URL.RawQuery, string(body), user, pass, r.Header.Get("Content-Type")})
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL+"/", "bot@example.com", "secret-token", time.Second), &reqs
}

func TestClient_Requests(t *testing.T) {
	client, reqs := fakeJira(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/transitions"):
			_ = json.NewEncoder(w).Encode(map[string]any{"transitions": transitions("In Review")})
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"fields":{"status":{"name":"In Progress"}}}`)
		case strings.HasSuffix(r.URL.Path, "/comment"):
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"1"}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})

	status, err := client.GetStatus("ABC-1")
	if err != nil || status != "In Progress" {
		t.Fatalf("GetStatus = (%q, %v)", status, err)
	}
	if err := client.AddComment("ABC-1", "Build [42|https://x] succeeded."); err != nil {
		t.Fatal(err)
	}
	ts, err := client.Transitions("ABC-1")
	if err != nil || len(ts) != 1 || ts[0].To.Name != "In Review" {
		t.Fatalf("Transitions = (%+v, %v)", ts, err)
	}
	if err := client.Transition("ABC-1", "1"); err != nil {
		t.Fatal(err)
	}

	want := []recorded{
		{method: "GET", path: "/rest/api/3/issue/ABC-1", query: "fields=status"},
		{method: "POST", path: "/rest/api/2/issue/ABC-1/comment", body: `{"body":"Build [42|https://x] succeeded."}`, contentType: "application/json"},
		{method: "GET", path: "/rest/api/3/issue/ABC-1/transitions"},
		{method: "POST", path: "/rest/api/3/issue/ABC-1/transitions", body: `{"transition":{"id":"1"}}`, contentType: "application/json"},
	}
	if len(*reqs) != len(want) {
		t.Fatalf("got %d requests, want %d", len(*reqs), len(want))
	}
	for i, got := range *reqs {
		if got.user != "bot@example.com" || got.pass != "secret-token" {
			t.Errorf("request %d: basic auth = (%q, %q)", i, got.user, got.pass)
		}
		got.user, got.pass = "", ""
		if got != want[i] {
			t.Errorf("request %d:\n got  %+v\n want %+v", i, got, want[i])
		}
	}
}

func TestClient_IssueURL(t *testing.T) {
	c := NewClient("https://your-site.atlassian.net/", "", "", time.Second)
	if got := c.IssueURL("ABC-1"); got != "https://your-site.atlassian.net/browse/ABC-1" {
		t.Errorf("got %q", got)
	}
}

func TestClient_ErrorStatuses(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{http.StatusUnauthorized, "jira_api_token"},
		{http.StatusForbidden, "can't see it"},
		{http.StatusNotFound, "doesn't exist"},
		{http.StatusInternalServerError, "returned 500"},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.code), func(t *testing.T) {
			client, _ := fakeJira(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.code)
				_, _ = io.WriteString(w, `{"errorMessages":["nope"]}`)
			})
			err := client.Transition("ABC-1", "1")

			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != tt.code {
				t.Fatalf("got %v, want an APIError with status %d", err, tt.code)
			}
			for _, want := range []string{tt.want, "POST /rest/api/3/issue/ABC-1/transitions", "nope"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q doesn't contain %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "secret-token") {
				t.Error("error contains the API token")
			}
		})
	}
}

func TestClient_Timeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	client := NewClient(srv.URL, "bot@example.com", "secret-token", 50*time.Millisecond)
	_, err := client.GetStatus("ABC-1")
	if err == nil || !strings.Contains(err.Error(), "didn't respond within 50ms") {
		t.Fatalf("got %v, want a timeout error", err)
	}
}

func TestBearerClient(t *testing.T) {
	type req struct{ path, auth string }
	var reqs []req
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, req{r.URL.Path, r.Header.Get("Authorization")})
		_, _ = io.WriteString(w, `{"fields":{"status":{"name":"In Progress"}}}`)
	}))
	t.Cleanup(srv.Close)
	gateway := apiGatewayURL
	apiGatewayURL = srv.URL + "/"
	t.Cleanup(func() { apiGatewayURL = gateway })

	client := NewBearerClient("https://your-site.atlassian.net/", "cloud-123", "bearer-token", time.Second)
	if _, err := client.GetStatus("ABC-1"); err != nil {
		t.Fatal(err)
	}

	want := req{"/ex/jira/cloud-123/rest/api/3/issue/ABC-1", "Bearer bearer-token"}
	if len(reqs) != 1 || reqs[0] != want {
		t.Errorf("got %+v, want [%+v]", reqs, want)
	}
	// Browse links still point at the site: the gateway has no /browse pages.
	if got := client.IssueURL("ABC-1"); got != "https://your-site.atlassian.net/browse/ABC-1" {
		t.Errorf("IssueURL = %q", got)
	}
}

func TestLookupCloudID(t *testing.T) {
	tests := []struct {
		name    string
		code    int
		body    string
		want    string
		wantErr string
	}{
		{"found", http.StatusOK, `{"cloudId":"cloud-123"}`, "cloud-123", ""},
		{"not a Jira site", http.StatusNotFound, ``, "", "returned 404"},
		{"no cloud ID", http.StatusOK, `{}`, "", "no cloudId"},
		{"not JSON", http.StatusOK, `<html>`, "", "unexpected response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/_edge/tenant_info" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected request %s, Authorization %q", r.URL.Path, r.Header.Get("Authorization"))
				}
				w.WriteHeader(tt.code)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(srv.Close)

			got, err := LookupCloudID(srv.URL+"/", time.Second)
			if tt.wantErr == "" {
				if err != nil || got != tt.want {
					t.Errorf("got (%q, %v), want %q", got, err, tt.want)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got (%q, %v), want an error containing %q", got, err, tt.wantErr)
			}
		})
	}
}
