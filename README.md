# mwcli

**A MediaWiki CLI for editors: log in, read and edit pages on any wiki.**

`mw` works with any MediaWiki wiki, Wikipedia included. Log in once with an OAuth token or a password, then view and edit pages from the command line; one binary, nothing else to install. For anything else, `mw api` makes Action API requests.

```sh
mw auth login --wiki mediawiki.org
mw auth status
mw page view 'Main Page'
mw page view 'Main Page' --json pageid,url,revision
mw page list --category 'Living people' -L 10
mw page edit 'Sandbox' --summary 'Fix typo' --file page.wiki
mw api action=query meta=userinfo uiprop=rights
```

It runs on macOS, Windows and Linux.

## Documentation

- [Getting started](docs/getting-started.md): install mw, connect to a wiki, log in and edit a page
- [Choosing a wiki](docs/wikis.md): built-in sites, `mw site add`, `--wiki` and the default site
- [Authentication](docs/auth.md): choosing between OAuth 2 tokens, OAuth 1.0a consumers, bot passwords and passwords, keeping them secure
- [Pages](docs/pages.md): `mw page view` with `--json`, `--jq` and `--web`, `mw page list`, `mw page history`, `mw page diff`, `mw page create`, `mw page edit` and `mw browse`
- [API requests](docs/api.md): `mw api`, with `--paginate` and `--jq`
- [Commands](docs/command.md): the command tree, environment variables and exit status
- [Example configuration](config.example.yaml): farms, aliases and login structure

Every command also prints its own help with `mw <command> --help`.

## Contributing

See [AGENTS.md](https://github.com/mwcli/mwcli/blob/main/AGENTS.md) for how changes are designed and [Releasing](docs/releasing.md) for how versions are published.

## Installation

### Windows

```sh
winget install mwcli.mwcli
```

### macOS and Linux

Download an archive from the [releases](https://github.com/mwcli/mwcli/releases) page, or install with Go:

```sh
go install github.com/mwcli/mwcli/cmd/mw@latest
```

### Build from source

Building requires Go 1.27 or newer. `make` builds `bin/mw`, and `make install` installs it (`prefix=/usr/local` by default). `mw --version` shows the version.

### Verification of binaries

Each release has a `checksums.txt` with the SHA-256 of its archives. Download it next to the archive and compare.

On Linux:

```sh
sha256sum --check --ignore-missing checksums.txt
```

On macOS, print the hash of the archive and check that `checksums.txt` has the same value for that file name:

```sh
shasum -a 256 mw_0.4.2_darwin_arm64.tar.gz
grep mw_0.4.2_darwin_arm64.tar.gz checksums.txt
```

On Windows (PowerShell), compare the same way; `Get-FileHash` prints the hash in upper case:

```powershell
Get-FileHash mw_0.4.2_windows_amd64.zip -Algorithm SHA256
Select-String mw_0.4.2_windows_amd64.zip checksums.txt
```

## Similar projects

Command-line tools:

| Project | Command | Language | Works with |
| --- | --- | --- | --- |
| [a Wikimedia CLI](https://gitlab.wikimedia.org/repos/releng/cli) | `wm`, formerly `mw` | Go | Wikimedia services and a MediaWiki development environment |
| [MediaWiki CLI](https://mwcli.toolforge.org/) by Sam Wilson | `mwcli` | PHP | any MediaWiki wiki: uploads, downloads, page exports |
| [wikibase-cli](https://codeberg.org/maxlath/wikibase-cli) | `wb` | JavaScript | Wikidata and other Wikibase instances |

Libraries and frameworks:

| Project | Language | Works with |
| --- | --- | --- |
| [Pywikibot](https://www.mediawiki.org/wiki/Manual:Pywikibot) | Python | any MediaWiki wiki: a bot framework with maintenance scripts (`pwb`) |
| [mwclient](https://github.com/mwclient/mwclient) | Python | the MediaWiki API |
| [mwcli on PyPI](https://pypi.org/project/mwcli/) | Python | Python helpers for building MediaWiki command-line tools |

a Wikimedia CLI still installs `mw` while it moves to `wm`; if you use both, run that one as `wm`. Sam Wilson's MediaWiki CLI and the PyPI package share the name mwcli but are separate projects. mwcli is not an official MediaWiki or Wikimedia project.
