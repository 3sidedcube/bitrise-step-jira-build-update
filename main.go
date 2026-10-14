package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bitrise-io/go-steputils/v2/export"
	"github.com/bitrise-io/go-steputils/v2/stepconf"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	requestTimeout = 10 * time.Second
	// Filled in by the step, since Bitrise doesn't know the source branch.
	sourceBranchPlaceholder = "{source_branch}"
)

// Config holds the step inputs plus the Bitrise variables the step reads.
type Config struct {
	JiraBaseURL     string          `env:"jira_base_url,required"`
	JiraAuthType    string          `env:"jira_auth_type,opt[basic,bearer]"`
	JiraUserEmail   stepconf.Secret `env:"jira_user_email"`
	JiraAPIToken    stepconf.Secret `env:"jira_api_token,required"`
	JiraCloudID     string          `env:"jira_cloud_id"`
	TargetStatus    string          `env:"target_status,required"`
	Comment         string          `env:"comment"`
	SourceBranch    string          `env:"source_branch"`
	JiraProjectKeys string          `env:"jira_project_keys"`
	FailIfNoIssue   bool            `env:"fail_if_no_issue,opt[yes,no]"`

	CommitSubject string `env:"GIT_CLONE_COMMIT_MESSAGE_SUBJECT"`
	GitBranch     string `env:"BITRISE_GIT_BRANCH"`
}

func main() {
	logger := log.NewLogger()
	if err := run(logger); err != nil {
		logger.Errorf("%s", err)
		os.Exit(1)
	}
}

func run(logger log.Logger) error {
	envRepo := env.NewRepository()

	var cfg Config
	if err := stepconf.NewInputParser(envRepo).Parse(&cfg); err != nil {
		return fmt.Errorf("invalid inputs: %w", err)
	}
	stepconf.Print(cfg)
	fmt.Println()
	if err := validateAuth(cfg); err != nil {
		return fmt.Errorf("invalid inputs: %w", err)
	}

	exporter := export.NewDefaultExporter(command.NewFactory(envRepo))

	branch, source := DetectSourceBranch(cfg.SourceBranch, cfg.CommitSubject, cfg.GitBranch)
	if source == SourceNone {
		logger.Warnf("Couldn't work out the source branch: source_branch, the merge commit subject and BITRISE_GIT_BRANCH are all empty or unrecognised.")
	} else {
		logger.Infof("Source branch %q (from %s)", branch, source)
	}

	key := FindIssueKey(branch, ParseProjectKeys(cfg.JiraProjectKeys))
	if key == "" {
		if err := exportOutputs(exporter, "", ""); err != nil {
			return err
		}
		msg := fmt.Sprintf("No Jira ticket key found in branch %q", branch)
		if cfg.JiraProjectKeys != "" {
			msg += fmt.Sprintf(" for projects %s", cfg.JiraProjectKeys)
		}
		if cfg.FailIfNoIssue {
			return fmt.Errorf("%s (fail_if_no_issue is yes)", msg)
		}
		logger.Warnf("%s. Nothing to update.", msg)
		return nil
	}

	client, err := newClient(logger, cfg)
	if err != nil {
		return err
	}
	issueURL := client.IssueURL(key)
	logger.Infof("Jira ticket %s (%s)", key, issueURL)
	if err := exportOutputs(exporter, key, issueURL); err != nil {
		return err
	}

	// Comment first: if the move then fails, the build number is still on the ticket.
	comment := strings.ReplaceAll(cfg.Comment, sourceBranchPlaceholder, branch)
	if strings.TrimSpace(comment) == "" {
		logger.Printf("comment is empty, not commenting.")
	} else {
		if err := client.AddComment(key, comment); err != nil {
			return fmt.Errorf("commenting on %s: %w", key, err)
		}
		logger.Donef("Commented on %s", key)
	}

	return moveIssue(logger, client, key, cfg.TargetStatus)
}

// validateAuth checks the inputs each auth type needs, before any request is made.
func validateAuth(cfg Config) error {
	if cfg.JiraAuthType != "bearer" && strings.TrimSpace(string(cfg.JiraUserEmail)) == "" {
		return errors.New("jira_user_email is required when jira_auth_type is basic")
	}
	return nil
}

func newClient(logger log.Logger, cfg Config) (*Client, error) {
	token := string(cfg.JiraAPIToken)
	if cfg.JiraAuthType != "bearer" {
		return NewClient(cfg.JiraBaseURL, string(cfg.JiraUserEmail), token, requestTimeout), nil
	}

	cloudID := strings.TrimSpace(cfg.JiraCloudID)
	if cloudID == "" {
		id, err := LookupCloudID(cfg.JiraBaseURL, requestTimeout)
		if err != nil {
			return nil, fmt.Errorf("looking up the Jira cloud ID (set jira_cloud_id to skip this): %w", err)
		}
		logger.Printf("Jira cloud ID %s, looked up from jira_base_url", id)
		cloudID = id
	}
	return NewBearerClient(cfg.JiraBaseURL, cloudID, token, requestTimeout), nil
}

func moveIssue(logger log.Logger, client *Client, key, target string) error {
	status, err := client.GetStatus(key)
	if err != nil {
		return fmt.Errorf("reading the status of %s: %w", key, err)
	}
	if strings.EqualFold(status, strings.TrimSpace(target)) {
		logger.Donef("%s is already in %q, not moving it.", key, status)
		return nil
	}

	transitions, err := client.Transitions(key)
	if err != nil {
		return fmt.Errorf("listing transitions for %s: %w", key, err)
	}
	t, err := FindTransition(transitions, target)
	if err != nil {
		return fmt.Errorf("moving %s from %q: %w", key, status, err)
	}
	if err := client.Transition(key, t.ID); err != nil {
		return fmt.Errorf("moving %s from %q to %q: %w", key, status, t.To.Name, err)
	}
	logger.Donef("Moved %s from %q to %q", key, status, t.To.Name)
	return nil
}

func exportOutputs(exporter export.Exporter, key, issueURL string) error {
	for name, value := range map[string]string{"JIRA_ISSUE_KEY": key, "JIRA_ISSUE_URL": issueURL} {
		if err := exporter.ExportOutput(name, value); err != nil {
			return fmt.Errorf("exporting %s: %w", name, err)
		}
	}
	return nil
}
