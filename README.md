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

- `cancel-releases`
- `clone-pipeline`
- `clone-folder`
- `compare-pipelines`
- `create-files`
- `create-powershell-pipeline`
- `delete-pipelines`
- `delete-pipeline-steps`
- `list-pool-members`
- `list-pools`
- `list-releases`
- `rename-or-move-pipelines`
- `replace-pipeline-content`
- `list-pipeline-agent-job`
- `list-pipeline-schedule`
- `list-pipeline-steps`
- `list-pipeline-variables`
- `list-release-history`
- `list-release-status`
- `trigger-release`
- `update-pipeline-agent-job`
- `update-pipeline-demands`
- `update-pipeline-schedule`
- `update-pipeline-variables`

Use `--dry-run` before applying batch changes. Commands that make changes may also require `--yes` (or `-y`) to skip interactive confirmation.

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
