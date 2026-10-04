# Releasing a22r

Tagged releases are built by `.github/workflows/release.yml`. GoReleaser publishes:

- macOS, Linux, and Windows archives to the GitHub release;
- DEB and RPM packages for Linux `amd64` and `arm64`;
- an `a22r` cask to `theomerkaratas/homebrew-tap`.

## One-time Homebrew setup

Create a fine-grained GitHub personal access token with access only to the
`theomerkaratas/homebrew-tap` repository and `Contents: Read and write`
permission. Add it to the `azure-devops-batch-operator` repository as an
Actions secret named `HOMEBREW_TAP_GITHUB_TOKEN`.

The default `GITHUB_TOKEN` publishes GitHub release assets, but GitHub does not
allow it to write to the separate tap repository.

## Publish a release

Ensure the main branch is clean and validated, then create and push a semantic
version tag:

```sh
git tag v1.0.0
git push origin v1.0.0
```

The release workflow validates the project, creates the GitHub release and
packages, and updates `Casks/a22r.rb` in the tap.

## APT and RPM repositories

GitHub release assets can be installed directly with `apt`, `apt-get`, `dnf`,
or `yum` after download, as documented in the main README. Package-name-only
commands such as `apt install a22r` require publishing the DEB/RPM artifacts to
a signed repository such as Cloudsmith or Packagecloud. That repository needs
its own account, signing configuration, and Actions secret before it can be
added to this workflow.
