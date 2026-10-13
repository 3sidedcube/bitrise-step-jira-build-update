package main

import "testing"

func TestDetectSourceBranch(t *testing.T) {
	tests := []struct {
		name       string
		override   string
		subject    string
		gitBranch  string
		wantBranch string
		wantSource BranchSource
	}{
		{
			name:       "PR merge subject",
			subject:    "Merge pull request #541 from my-org/fix/ABC-123-login-button",
			gitBranch:  "develop",
			wantBranch: "fix/ABC-123-login-button",
			wantSource: SourceCommitSubject,
		},
		{
			name:       "merge branch subject",
			subject:    "Merge branch 'fix/ABC-123-login-button' into develop",
			gitBranch:  "develop",
			wantBranch: "fix/ABC-123-login-button",
			wantSource: SourceCommitSubject,
		},
		{
			name:       "manual build on a feature branch",
			subject:    "Fix the login button",
			gitBranch:  "fix/ABC-123-login-button",
			wantBranch: "fix/ABC-123-login-button",
			wantSource: SourceGitBranch,
		},
		{
			name:       "override wins",
			override:   " feature/ABC-1-x ",
			subject:    "Merge pull request #541 from my-org/fix/ABC-123-login-button",
			gitBranch:  "develop",
			wantBranch: "feature/ABC-1-x",
			wantSource: SourceInput,
		},
		{
			name:       "squash merge falls back to the target branch",
			subject:    "Fix the login button (#541)",
			gitBranch:  "develop",
			wantBranch: "develop",
			wantSource: SourceGitBranch,
		},
		{
			name:       "nothing available",
			wantSource: SourceNone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			branch, source := DetectSourceBranch(tt.override, tt.subject, tt.gitBranch)
			if branch != tt.wantBranch || source != tt.wantSource {
				t.Errorf("got (%q, %q), want (%q, %q)", branch, source, tt.wantBranch, tt.wantSource)
			}
		})
	}
}

func TestBranchFromMergeSubject_Squash(t *testing.T) {
	if got := BranchFromMergeSubject("ABC-123 Fix the login button (#541)"); got != "" {
		t.Errorf("got %q, want no branch", got)
	}
}

func TestFindIssueKey(t *testing.T) {
	tests := []struct {
		branch   string
		projects string
		want     string
	}{
		{"fix/ABC-123-login-button", "", "ABC-123"},
		{"fix/abc-123-login-button", "", "ABC-123"},
		{"ABC-123", "", "ABC-123"},
		{"feature/APP2-532-android-17-upgrade", "", "APP2-532"},
		{"feature/ABC-1-and-ABC-2", "", "ABC-1"},
		{"feature/APP2-532-android-17-upgrade", "APP2", "APP2-532"},
		{"feature/APP2-532-android-17-upgrade", "android", "ANDROID-17"},
		{"chore/android-17-upgrade", "", "ANDROID-17"},
		{"chore/android-17-upgrade", "ABC", ""},
		{"fix/xyz-12-x", "ABC, XYZ", "XYZ-12"},
		{"fix/fooABC-12", "ABC", ""},
		{"develop", "", ""},
		{"release/2.3.0", "", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.branch+"|"+tt.projects, func(t *testing.T) {
			if got := FindIssueKey(tt.branch, ParseProjectKeys(tt.projects)); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
