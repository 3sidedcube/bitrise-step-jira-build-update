package main

import (
	"regexp"
	"strings"
)

// BranchSource says where the source branch was found.
type BranchSource string

const (
	SourceInput         BranchSource = "source_branch input"
	SourceCommitSubject BranchSource = "merge commit subject"
	SourceGitBranch     BranchSource = "BITRISE_GIT_BRANCH"
	SourceNone          BranchSource = "none"
)

var (
	// Merge pull request #541 from my-org/fix/ABC-123-login-button
	prMergePattern = regexp.MustCompile(`^Merge pull request #\d+ from [^/\s]+/(\S+)`)
	// Merge branch 'fix/ABC-123-login-button' into develop
	branchMergePattern = regexp.MustCompile(`^Merge branch '([^']+)'`)
	// A Jira key such as ABC-123 or APP2-532. The key must not follow a letter or digit,
	// so "fooABC-1" isn't read as a key. Matched case-insensitively and upper-cased after.
	issueKeyPattern = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])([a-z][a-z0-9_]*-\d+)`)
)

// DetectSourceBranch works out the branch the build's changes came from. It uses, in order:
// the source_branch input, the merge commit subject, then BITRISE_GIT_BRANCH.
func DetectSourceBranch(override, commitSubject, gitBranch string) (string, BranchSource) {
	if b := strings.TrimSpace(override); b != "" {
		return b, SourceInput
	}
	if b := BranchFromMergeSubject(commitSubject); b != "" {
		return b, SourceCommitSubject
	}
	if b := strings.TrimSpace(gitBranch); b != "" {
		return b, SourceGitBranch
	}
	return "", SourceNone
}

// BranchFromMergeSubject returns the source branch named in a GitHub PR merge or
// "Merge branch" commit subject, or "" for any other subject (e.g. squash merges).
func BranchFromMergeSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	for _, p := range []*regexp.Regexp{prMergePattern, branchMergePattern} {
		if m := p.FindStringSubmatch(subject); m != nil {
			return m[1]
		}
	}
	return ""
}

// ParseProjectKeys turns "abc, xyz" into ["ABC", "XYZ"].
func ParseProjectKeys(list string) []string {
	var keys []string
	for _, k := range strings.Split(list, ",") {
		if k = strings.ToUpper(strings.TrimSpace(k)); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// FindIssueKey returns the first Jira key in branch, upper-cased. When projectKeys is
// non-empty, only keys from those projects count. Returns "" if there's no match.
func FindIssueKey(branch string, projectKeys []string) string {
	for _, m := range issueKeyPattern.FindAllStringSubmatch(branch, -1) {
		key := strings.ToUpper(m[1])
		if len(projectKeys) == 0 {
			return key
		}
		project := key[:strings.LastIndex(key, "-")]
		for _, p := range projectKeys {
			if project == p {
				return key
			}
		}
	}
	return ""
}
