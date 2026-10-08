# Azure DevOps Batch Operator (`a22r`)

`a22r` is a terminal application for inspecting and updating Azure DevOps classic release pipelines in bulk. It provides an interactive terminal UI and individual commands suitable for scripts.

## Features

- Browse release pipeline folders and definitions.
- Inspect variables, schedules, agent jobs, tasks, release history, and status.
- Compare, clone, rename, or move pipelines.
- Update variables, schedules, agent jobs, and demands.
- Trigger or cancel releases.
- Preview supported write operations with `--dry-run`.

Run `a22r` without arguments to open the interactive UI, or use `a22r <command> ...` to execute one command.

## Installation

### macOS and Linux

Install with Homebrew:

```sh
brew install --cask theomerkaratas/tap/a22r
```

The first installation adds the `theomerkaratas/homebrew-tap` tap automatically. Subsequent updates are available through `brew upgrade a22r`.

### Debian and Ubuntu

Every release includes native `amd64` and `arm64` DEB packages. Set the release version and use the architecture reported by `dpkg`:

```sh
VERSION=1.0.0
ARCH="$(dpkg --print-architecture)"
curl -fLO "https://github.com/theomerkaratas/azure-devops-batch-operator/releases/download/v${VERSION}/a22r_${VERSION}_linux_${ARCH}.deb"
sudo apt install "./a22r_${VERSION}_linux_${ARCH}.deb"
```

The same downloaded package can be installed with `apt-get`:

```sh
sudo apt-get install "./a22r_${VERSION}_linux_${ARCH}.deb"
```

### Fedora, RHEL, Rocky Linux, AlmaLinux, and Amazon Linux

Every release also includes RPM packages. Select the architecture matching your machine (`amd64` or `arm64` in the asset name):

```sh
VERSION=1.0.0
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
curl -fLO "https://github.com/theomerkaratas/azure-devops-batch-operator/releases/download/v${VERSION}/a22r_${VERSION}_linux_${ARCH}.rpm"
sudo dnf install "./a22r_${VERSION}_linux_${ARCH}.rpm"
```

On systems that use `yum`:

```sh
sudo yum install "./a22r_${VERSION}_linux_${ARCH}.rpm"
```

### Install script

Install the latest release with `curl`:

```sh
curl -fsSL https://raw.githubusercontent.com/theomerkaratas/azure-devops-batch-operator/main/install.sh | sh
```

Or with `wget`:

```sh
wget -qO- https://raw.githubusercontent.com/theomerkaratas/azure-devops-batch-operator/main/install.sh | sh
```

Install a specific version:

```sh
curl -fsSL https://raw.githubusercontent.com/theomerkaratas/azure-devops-batch-operator/main/install.sh | sh -s -- v1.0.0
```

The installer supports Linux and macOS on `amd64` and `arm64`. It verifies the archive against the published SHA-256 checksums and installs the binary into `/usr/local/bin` or `~/.local/bin`.

Running the installer again updates the binary while preserving an existing configuration file.

Package-manager artifacts and the Homebrew cask are published by the tagged-release workflow. Maintainer setup is documented in [docs/releasing.md](docs/releasing.md).

### Windows

Download `a22r_windows_amd64.zip` or `a22r_windows_arm64.zip` from [GitHub Releases](https://github.com/omerkaratas/azure-devops-batch-operator/releases). Extract `a22r.exe` into a directory on `PATH` and copy `config.example.yml` to the configuration location below as `config.yml`.

## Configuration

The macOS/Linux installer creates a private configuration template with file mode `0600`. Print the path used by the application with:

```console
a22r config-path
```

Default locations:

- macOS and Linux: `${XDG_CONFIG_HOME:-~/.config}/a22r/config.yml` (legacy macOS path `~/Library/Application Support/a22r/config.yml` is also supported)
- Windows: `%AppData%\a22r\config.yml`

Edit the file and provide only the tokens required by the operations you use:

```yaml
deployment: cloud
azure_devops_url: "https://dev.azure.com/your-organization"

# read, read-write, or manage
default_token: read

tokens:
  read: "your-read-token"
  read_write: "your-read-write-token"
  manage: "your-manage-token"
```

For an on-prem Azure DevOps installation, select `on-prem` and provide the server URL and collection:

```yaml
deployment: on-prem
azure_devops_url: "https://devops.example.com/tfs"
collection: "DefaultCollection"

default_token: read
tokens:
  read: "your-read-token"
  read_write: "your-read-write-token"
  manage: "your-manage-token"
```

`deployment` defaults to `on-prem` for compatibility with configurations created before cloud support was added. `collection` is used only for `on-prem` deployments and defaults to `DefaultCollection`.

Treat this file as a secret. Do not commit it or share its contents. The repository's [config.example.yml](config.example.yml) (cloud) and [config.onprem.example.yml](config.onprem.example.yml) (on-prem) contain empty templates safe to copy.

### Configuration precedence

Values are resolved in this order, from highest to lowest priority:

1. The command's `--level` option.
2. Environment variables.
3. Values in `config.yml`.
4. The command's built-in fallback level.

Supported environment variables:

- `A22R_CONFIG`: use a custom configuration file path.
- `A22R_DEPLOYMENT`: override `deployment` (`cloud` or `on-prem`).
- `A22R_COLLECTION`: override the on-prem collection name.
- `A22R_DEFAULT_TOKEN`: override `default_token`.
- `AZURE_DEVOPS_URL`: override `azure_devops_url`.
- `AZURE_DEVOPS_PAT_READ`: override `tokens.read`.
- `AZURE_DEVOPS_PAT_READWRITE`: override `tokens.read_write`.
- `AZURE_DEVOPS_PAT_MANAGE`: override `tokens.manage`.

## Usage

Open the interactive UI:

```console
a22r
```

List available commands or display command-specific help:

```console
a22r --help
a22r list-releases --help
```

Example read operation:

```console
a22r list-releases 'Example.Project\TEST\CONFIG'
```

Example write preview:

```console
a22r update-pipeline-variables 'Example.Project\TEST\CONFIG' \
  --set Environment=test \
  --dry-run
```

Pass `--level read`, `--level read-write`, or `--level manage` to select a particular token for one command.

## Commands

- `audit-pipeline-permissions`
- `audit-pipeline-policy`
- `cancel-releases`
- `cleanup-releases`
- `clone-pipeline`
- `clone-folder`
- `compare-pipelines`
- `create-files`
- `create-powershell-pipeline`
- `delete-pipelines`
- `delete-pipeline-steps`
- `detect-pipeline-variable-conflicts`
- `enforce-pipeline-policy`
- `list-pool-members`
- `list-pools`
- `list-releases`
- `rename-or-move-pipelines`
- `replace-pipeline-content`
- `replace-pipeline-variable-groups`
- `list-pipeline-agent-job`
- `list-pipeline-schedule`
- `list-pipeline-steps`
- `list-pipeline-variables`
- `list-release-history`
- `list-release-status`
- `detect-broken-artifact-references`
- `detect-deprecated-tasks`
- `update-pipeline-cd-triggers`
- `upgrade-pipeline-tasks`
- `manage-pipeline-stages`
- `copy-pipeline-stage`
- `synchronize-pipelines`
- `orchestrate-releases`
- `trigger-release`
- `update-pipeline-agent-job`
- `update-pipeline-approvals`
- `update-pipeline-artifacts`
- `update-pipeline-stage-triggers`
- `update-pipeline-demands`
- `update-pipeline-retention`
- `update-pipeline-gates`
- `update-pipeline-schedule`
- `update-pipeline-variables`
- `update-pipeline-variable-groups`
- `report-pipeline-inventory`
- `detect-pipeline-drift`
- `resume-batch`
- `rollback-batch`
- `list-batch-manifests`

Use `--dry-run` before applying batch changes. Commands that make changes may also require `--yes` (or `-y`) to skip interactive confirmation.

Every batch update that saves changes first writes an operation manifest to a `manifests` folder
beside the configuration file (override with `A22R_MANIFEST_DIR`; files are owner-only because
planned definitions can contain secret values you set). It records the target pipelines, their original
revisions and definitions, the planned changes, and each result or failure. Dry runs write nothing.

```console
a22r list-batch-manifests                 # list manifests, or pass an ID to see details
a22r resume-batch 20260101-120000-ab12cd  # finish pending/failed pipelines only
a22r rollback-batch 20260101-120000-ab12cd --dry-run
```

`resume-batch` skips pipelines already updated and applies the rest only if their revision is still
the one the operation read. `rollback-batch` restores the captured definitions, but only for
pipelines whose current revision is still the one the operation produced; anything edited since is
reported and left alone. Secret variable values are masked by Azure DevOps, so a rollback cannot
recover them.

Produce an inventory of every pipeline under a folder for compliance reviews or migration planning:

```console
a22r report-pipeline-inventory 'Example.Project\TEST' --format csv --out inventory.csv
a22r report-pipeline-inventory 'Example.Project' --format json --out inventory.json
```

The report covers stages (rank, conditions, pre/post approvals, retention), jobs (pool, demands,
timeout), tasks (name and version), variables and variable groups, artifacts, triggers and
schedules. `--format` is `text` (default), `json`, or `csv` (one row per pipeline stage). Variable
names and secret flags are listed; values are left out unless you pass `--include-values` (JSON),
and secret values are never available.

Find configuration drift across a whole folder instead of comparing pipelines two at a time:

```console
a22r detect-pipeline-drift 'Example.Project\TEST'
a22r detect-pipeline-drift 'Example.Project\TEST' --reference 'Example.Project\GOLDEN\Deploy'
a22r detect-pipeline-drift 'Example.Project\TEST' --write-baseline baseline.json
a22r detect-pipeline-drift 'Example.Project\TEST' --baseline baseline.json --fail-on-drift
```

Without `--reference` or `--baseline`, the baseline is the folder's own consensus: each setting's
most common value, counted only when at least `--min-agreement` percent (default 60) of the
pipelines agree. The report lists recurring drift patterns (one setting, how many pipelines share
it), groups of pipelines that drift identically, and the drift count per pipeline. Edit a baseline
written with `--write-baseline` to define your own standard, and use `--fail-on-drift` in CI.

Synchronize selected parts of matching pipelines from a golden reference definition:

```console
a22r synchronize-pipelines 'Example.Project\TEST' \
  --reference 'Example.Project\GOLDEN\Deploy' \
  --components steps,agent-settings \
  --dry-run
```

Components are `variables`, `steps`, `jobs`, `stages`, `agent-settings`, `approvals`, or `all`.
Nested components match stages and jobs by exact name. Secret values masked by Azure DevOps are
preserved in targets instead of being copied. Components containing agent settings require the
reference and targets to be in the same project because Azure DevOps queue IDs are project-specific.

Add, clone, rename, remove, or reorder stages across many pipelines. Stage ranks stay contiguous,
renames and removals update stages that depend on them (`--rewire` on remove), and any pipeline where
the result would break dependency order is reported and left untouched:

```console
a22r manage-pipeline-stages 'Example.Project\TEST' --action add --stage QA --after Dev --dry-run
a22r manage-pipeline-stages 'Example.Project\TEST' --action clone --stage Prod --new-name Prod-EU
```

Copy one complete stage (jobs, tasks, variables, conditions, approvals, retention) from a source
pipeline into the targets. `--on-existing` chooses `skip` (default), `replace`, or `rename`. The source
must be in the same project as the targets, and masked secret values must be set afterwards:

```console
a22r copy-pipeline-stage 'Example.Project\TEST' \
  --source 'Example.Project\GOLDEN\Deploy' --stage Production --on-existing replace --dry-run
```

Change when stages start with `update-pipeline-stage-triggers`: `after-release`, `after-stages`
(with `--after QA,Perf`), `manual`, or `sequential` to chain every stage after the one before it.
Use `--stage` to limit the change; pipelines where stage order would become invalid are skipped:

```console
a22r update-pipeline-stage-triggers 'Example.Project\TEST' --trigger sequential --dry-run
```

Replace build artifact sources in bulk with `update-pipeline-artifacts`. Match by alias, build
pipeline, project, or branch, then set a new build pipeline, project, branch, or alias
(`--rewrite-references` also updates `$(Release.Artifacts.<alias>.*)` usages):

```console
a22r update-pipeline-artifacts 'Example.Project\TEST' \
  --match-definition OldBuild --set-definition NewBuild --dry-run
```

Enable, disable, or filter continuous-deployment triggers (releases created when a build artifact
is published) with `update-pipeline-cd-triggers`. `--action` is `enable`, `disable`, or `set-filters`;
`--alias` limits the artifacts, and `--branch` / `--tag` set the build filters:

```console
a22r update-pipeline-cd-triggers 'Example.Project\TEST' \
  --action enable --branch refs/heads/main --dry-run
```

Find release pipelines likely to fail before deployment starts. `detect-broken-artifact-references`
checks that referenced build pipelines, Azure Repos repositories and branches, and service connections
still exist. Findings are `BROKEN` or `UNCHECKED` (could not be verified); `--fail-on-broken`
returns exit code 1 for CI:

```console
a22r detect-broken-artifact-references 'Example.Project\TEST' --fail-on-broken
```

Upgrade a task to another major version with `upgrade-pipeline-tasks`. `--task` takes a task name or
GUID. Before changing a step, its configured inputs are checked against the target version: removed
inputs and required inputs without a default are reported and the step is skipped unless `--force`
is given. Downgrades also need `--force`:

```console
a22r upgrade-pipeline-tasks 'Example.Project\TEST' --task PowerShell --to-version 2 --dry-run
```

Find pipelines at risk of future failures with `detect-deprecated-tasks`. It reports `MISSING`,
`UNSUPPORTED`, `DISABLED`, and `DEPRECATED` task versions (and `OUTDATED` with `--include-outdated`)
for enabled steps; `--fail-on-findings` returns exit code 1 for CI:

```console
a22r detect-deprecated-tasks 'Example.Project\TEST' --include-outdated
```

Standardize release retention with `update-pipeline-retention`: `--days`, `--releases`, and
`--retain-build true|false`, for every stage or only the ones named with `--stage`. Only the values
you pass are changed, and the organization's maximum retention settings still apply:

```console
a22r update-pipeline-retention 'Example.Project\TEST' --days 30 --releases 5 --retain-build=true --dry-run
```

Delete old release instances with `cleanup-releases`. It is a **dry run unless `--apply` is given**.
Choose releases with `--older-than DAYS` and/or `--status` (`succeeded`, `failed`, `canceled`,
`abandoned`, `draft`, `notdeployed`). Releases marked to be retained indefinitely and releases with a
deployment in progress are never deleted, and the newest `--keep-latest` (default 3) releases and
`--keep-successful` succeeded releases of each pipeline are always kept. Requires a `manage` token.
Deleted releases cannot be restored with this tool; Azure DevOps keeps them for the project's
"permanently destroy releases" period before destroying them for good:

```console
a22r cleanup-releases 'Example.Project\TEST' --older-than 90 --keep-successful 5
a22r cleanup-releases 'Example.Project\TEST' --older-than 90 --keep-successful 5 --apply
```

Roll out releases in controlled waves with `orchestrate-releases`. Pipelines are split into waves
of `--wave-size`; `--concurrency` bounds simultaneous releases within a wave and `--wave-delay`
pauses between waves. `--wait` waits for each deployment (`--timeout` minutes), `--stop-on-failure`
stops starting new releases after a failure, and transient errors (HTTP 429/5xx, network) are retried
with backoff (`--retries`). Each release is tagged so a retry never creates a duplicate. A summary of
every pipeline is printed at the end and the exit code is 1 if anything failed:

```console
a22r orchestrate-releases 'Example.Project\TEST' --wave-size 5 --wave-delay 60 --dry-run
a22r orchestrate-releases 'Example.Project\TEST' --wave-size 3 --concurrency 2 --wait --stop-on-failure -y
```

Audit who can view, edit, administer, trigger, approve, and delete release pipelines with
`audit-pipeline-permissions`. Effective permissions (deny wins, inheritance honoured) are grouped into
permission sets, and findings flag `BROAD` grants to groups such as "Valid Users" (`--broad-groups`),
`INCONSISTENT` permissions within a folder, and `NO-INHERIT` folders or pipelines. Group members are not
expanded, and the token must be allowed to read security information. `--fail-on-findings` returns
exit code 1 for CI:

```console
a22r audit-pipeline-permissions 'Example.Project\TEST' --broad-groups 'Valid Users,Contributors'
```

Link or unlink shared variable groups by numeric Azure DevOps group ID:

```console
a22r update-pipeline-variable-groups 'Example.Project\TEST' \
  --link 12 --unlink 7 --scope pipeline --dry-run
```

Replace an old variable-group reference with a new one across every pipeline in a folder (useful
for migrations, environment separation, or variable-group restructuring):

```console
a22r replace-pipeline-variable-groups 'Example.Project\TEST' \
  --map 12:34 --scope all --dry-run
```

Detect duplicate or conflicting variable names across pipeline variables, stage variables, and
linked variable groups, and see which value takes precedence:

```console
a22r detect-pipeline-variable-conflicts 'Example.Project\TEST'
```

Bulk-update pre/post-deployment approvers and approval policies (identity ids, not names, since
Azure DevOps approvals reference approvers by id; look an id up in the pipeline's Approvals UI or
via the Graph API). `--clear` combined with `--add` replaces the approver list outright:

```console
a22r update-pipeline-approvals 'Example.Project\TEST' \
  --phase pre --add 'a1b2c3d4-...:Jane Doe' --creator-can-approve=false \
  --timeout 1440 --stage Production --dry-run
```

Standardize pre/post-deployment gates (REST API checks, Azure Function checks, monitoring
queries, evaluation intervals, timeout behavior) across a folder by copying them verbatim from a
reference pipeline's name-matched stages:

```console
a22r update-pipeline-gates 'Example.Project\TEST' \
  --reference 'Example.Project\GOLDEN\Deploy' \
  --phase post --dry-run
```

Audit or enforce consistent pipeline policy across a folder:

```console
a22r audit-pipeline-policy 'Example.Project\TEST\CONFIG' \
  --policy policy.example.yml

a22r enforce-pipeline-policy 'Example.Project\TEST\CONFIG' \
  --policy policy.example.yml \
  --dry-run
```

The policy supports required and forbidden pipeline variables, required and forbidden step
titles, a required agent pool and demands, maximum job timeout, enabled steps, and minimum stage
retention. Missing required steps are reported but not automatically created because a title does
not provide the task type, version, inputs, or safe placement needed to construct one.

Clone a complete release folder tree, including its subfolders and release pipelines:

```console
a22r clone-folder 'Example.Project\DEV\CONFIG' \
  'Example.Project\TEST\CONFIG' \
  --dry-run
```

Delete every step with an exact, case-sensitive title from all release pipelines under a path:

```console
a22r delete-pipeline-steps 'Example.Project\TEST\CONFIG' \
  --title 'Obsolete deployment step' \
  --dry-run
```

Regex-replace repeating content in scripts, step titles, and/or variable values:

```console
a22r replace-pipeline-content 'Example.Project\TEST\CONFIG' \
  --find 'Service-(\w+)' \
  --replace 'App-$1' \
  --fields scripts,titles,variables \
  --dry-run
```

Create a classic release pipeline containing ordered, inline PowerShell tasks:

```console
a22r create-powershell-pipeline 'Example.Project\Deploy\Restart Services' \
  --script stop.ps1 \
  --script start.ps1 \
  --pool WindowsAgents \
  --dry-run
```

Replace `--pool WindowsAgents` with `--queue-id 42` when the project queue cannot be resolved by name. Add `--pwsh` to execute the tasks with PowerShell Core. The script files are embedded in the release definition, so the pipeline does not require a build artifact.

## Build from source

Go 1.24.2 or newer is required.

```sh
go build -o a22r ./cmd/a22r
go test ./...
```
