package main

import (
	"strings"
	"testing"
)

func TestValidateAuth(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"basic with email", Config{JiraAuthType: "basic", JiraUserEmail: "bot@example.com"}, false},
		{"basic without email", Config{JiraAuthType: "basic"}, true},
		{"basic with blank email", Config{JiraAuthType: "basic", JiraUserEmail: "  "}, true},
		{"bearer without email", Config{JiraAuthType: "bearer"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAuth(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("got %v, want error = %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "jira_user_email") {
				t.Errorf("error %q doesn't name the missing input", err)
			}
		})
	}
}
