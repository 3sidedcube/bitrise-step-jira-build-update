# Changelog

## 1.1.0

- Adds bearer token support: `jira_auth_type: bearer` sends `jira_api_token` as a bearer token through Atlassian's API gateway, which scoped, service account and OAuth tokens require. `jira_user_email` is now only required for `basic` auth.
- Adds the optional `jira_cloud_id` input. For `bearer`, it's looked up from `jira_base_url` when empty.

## 1.0.0

- First version: finds the ticket key from the merged PR's source branch, comments with the build number and install link, and moves the ticket to `target_status`.
