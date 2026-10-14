# Jira build update (Bitrise step)

A Bitrise step that runs after a successful build. It finds the Jira ticket from the source branch of the merged PR, comments on it with the build number and install link, and moves it to a status you choose, e.g. `In Review`.

## Usage

Add it to your app's `bitrise.yml` after `deploy-to-bitrise-io`, which sets the install link:

```yaml
    - deploy-to-bitrise-io@2: {}
    - git::https://github.com/3sidedcube/bitrise-step-jira-build-update.git@1.0.0:
        title: Update Jira ticket
        is_skippable: true          # a Jira problem fails this step, not the build
        inputs:
        - jira_base_url: $JIRA_BASE_URL
        - jira_user_email: $JIRA_USER_EMAIL
        - jira_api_token: $JIRA_API_TOKEN
        - target_status: In Review
```

- If `bitrise.yml` is stored on bitrise.io, paste the step into the Workflow Editor's **bitrise.yml** tab. The step search only lists Bitrise's own library.
- The tag pins the version. Your builds keep using it until you change the tag.

Each app needs three secrets: `JIRA_BASE_URL`, `JIRA_USER_EMAIL` and `JIRA_API_TOKEN` (an API token for the account the comments are posted as).

## Signing in to Jira

Atlassian has two kinds of token, and they're used differently:

- **Classic API token** (id.atlassian.com → Security → *Create API token*): use the default `jira_auth_type: basic`, with `jira_user_email` set to that account's email.
- **Bearer token** (a *token with scopes*, a service account token, or an OAuth token): set `jira_auth_type: bearer` and leave out `jira_user_email`. These tokens only work through Atlassian's API gateway, so the step sends requests to `https://api.atlassian.com/ex/jira/<cloud ID>`. It looks the cloud ID up from `jira_base_url`; set `jira_cloud_id` if that fails. The token needs the `read:jira-work` and `write:jira-work` scopes.

```yaml
        inputs:
        - jira_base_url: $JIRA_BASE_URL
        - jira_auth_type: bearer
        - jira_api_token: $JIRA_API_TOKEN
        - target_status: AI Review
```

A bearer token sent as `basic` (or to the site URL) gets a 401 or a misleading 404.

## How it finds the ticket

The source branch comes from the first of these that applies:

1. The `source_branch` input.
2. The merge commit subject: `Merge pull request #541 from my-org/fix/ABC-123-login-button` or `Merge branch 'fix/ABC-123-login-button' into develop`.
3. `$BITRISE_GIT_BRANCH`, for a build started by hand on a feature branch.

The first Jira key in the branch (e.g. `ABC-123`, case-insensitive) is the ticket. Squash and rebase merges don't name the source branch, so they aren't supported yet.

## Inputs

| Input | Required | Default | Notes |
|---|---|---|---|
| `jira_base_url` | yes | | e.g. `https://your-site.atlassian.net`. |
| `jira_auth_type` | no | `basic` | `basic` (classic API token + email) or `bearer`. See [Signing in to Jira](#signing-in-to-jira). |
| `jira_user_email` | for `basic` | | Secret. |
| `jira_api_token` | yes | | Secret. A classic API token or a bearer token. |
| `jira_cloud_id` | no | looked up | Only for `bearer`. |
| `target_status` | yes | | Status to move to, e.g. `In Review`. Not case-sensitive. |
| `comment` | no | build number, install link, branch, commit, build link | Jira wiki markup. `{source_branch}` is replaced with the detected branch. Empty skips commenting. |
| `source_branch` | no | | Overrides branch detection. |
| `jira_project_keys` | no | | Comma-separated, e.g. `ABC,XYZ`. Only keys from these projects count. |
| `fail_if_no_issue` | no | `no` | `yes` fails the step when the branch has no ticket key. |

## Outputs

| Output | Value |
|---|---|
| `JIRA_ISSUE_KEY` | e.g. `ABC-123`. Empty if none was found. |
| `JIRA_ISSUE_URL` | e.g. `https://your-site.atlassian.net/browse/ABC-123`. |

## Troubleshooting

- **"No Jira ticket key found"**: the branch has no key, or the build came from a squash merge. Set `source_branch`, or check the step log for the branch it found.
- **Picks the wrong key** (e.g. `ANDROID-17` from `chore/android-17-upgrade`): set `jira_project_keys`.
- **401**: check the API token secret, and the email for `basic`. "scope does not match" means a bearer token is missing a scope.
- **403 / 404**: the ticket doesn't exist, or the Jira account can't see it. Jira also answers 404 when it doesn't accept the credentials, e.g. a bearer token used with `basic` auth.
- **"can't be reached from the ticket's current status"**: the status name is wrong, or the workflow has no transition to it from where the ticket is. The error lists the statuses it can move to.

The comment is posted before the move, so a failed move still leaves the build number on the ticket.

## Development

```sh
bitrise run check   # gofmt, go vet, unit tests, step.yml audit
bitrise run test    # check + end-to-end run against a real sandbox ticket
```

For `bitrise run test`, copy `.bitrise.secrets.example.yml` to `.bitrise.secrets.yml` and fill it in. The test moves the sandbox ticket back afterwards and leaves one comment on it per run.

### Releasing

Tag with a semantic version (`1.0.0`, `1.0.1`, …) and add a `CHANGELOG.md` entry. Patch for fixes, minor for new optional inputs, major for renamed or removed inputs or changed failure behaviour. Never move or delete a published tag.
