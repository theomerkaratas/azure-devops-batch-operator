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

Install the latest release with `curl`:

```sh
curl -fsSL https://raw.githubusercontent.com/omerkaratas/azure-devops-batch-operator/main/install.sh | sh
```

Or with `wget`:

```sh
wget -qO- https://raw.githubusercontent.com/omerkaratas/azure-devops-batch-operator/main/install.sh | sh
```

Install a specific version:

```sh
curl -fsSL https://raw.githubusercontent.com/omerkaratas/azure-devops-batch-operator/main/install.sh | sh -s -- v1.0.0
```

The installer supports Linux and macOS on `amd64` and `arm64`. It verifies the archive against the published SHA-256 checksums and installs the binary into `/usr/local/bin` or `~/.local/bin`.

Running the installer again updates the binary while preserving an existing configuration file.

### Windows

Download `a22r_windows_amd64.zip` or `a22r_windows_arm64.zip` from [GitHub Releases](https://github.com/omerkaratas/azure-devops-batch-operator/releases). Extract `a22r.exe` into a directory on `PATH` and copy `config.example.yml` to the configuration location below as `config.yml`.

## Configuration

The macOS/Linux installer creates a private configuration template with file mode `0600`. Print the path used by the application with:

```console
a22r config-path
```

Default locations:

- macOS: `~/Library/Application Support/a22r/config.yml`
- Linux and Ubuntu: `${XDG_CONFIG_HOME:-~/.config}/a22r/config.yml`
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

Treat this file as a secret. Do not commit it or share its contents. The repository's [config.example.yml](config.example.yml) contains an empty template safe to copy.

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

- `cancel-releases`
- `clone-pipeline`
- `compare-pipelines`
- `create-files`
- `list-pools`
- `list-releases`
- `rename-or-move-pipelines`
- `show-pipeline-agent-job`
- `show-pipeline-schedule`
- `show-pipeline-steps`
- `show-pipeline-variables`
- `show-release-history`
- `show-release-status`
- `trigger-release`
- `update-pipeline-agent-job`
- `update-pipeline-demands`
- `update-pipeline-schedule`
- `update-pipeline-variables`

Use `--dry-run` before applying batch changes. Commands that make changes may also require `--yes` (or `-y`) to skip interactive confirmation.

## Build from source

Go 1.24.2 or newer is required.

```sh
go build -o a22r ./cmd/a22r
go test ./...
```
