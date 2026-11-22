# mwcli

**Unofficial but universal MediaWiki CLI. Like `gh`, but for MediaWiki.**

`mw` is MediaWiki on the command line. It logs in to any MediaWiki wiki, reads and edits pages and makes Action API requests.

```sh
mw auth login --wiki mediawiki.org
mw auth status
mw page view 'Main Page'
mw page view 'Main Page' --json pageid,url,revision
mw page edit 'Sandbox' --summary 'Fix typo' --file page.wiki
mw api action=query meta=userinfo uiprop=rights
```

mwcli works with any MediaWiki wiki, Wikimedia wikis included, on macOS, Windows and Linux.

## Documentation

- [Choosing a wiki](docs/wikis.md): `--wiki`, interwiki prefixes, API discovery
- [Authentication](docs/auth.md): OAuth 2 tokens, passwords, replacing a token, `MW_TOKEN`
- [Pages](docs/pages.md): `mw page view` with `--json`, `--jq` and `--web`, `mw page edit`
- [API requests](docs/api.md): `mw api`
- [Commands](docs/command.md): the command tree, environment variables and exit status

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
| mwcli (this project) | `mw` | Go | any MediaWiki wiki: pages, accounts, the Action API |
| [a Wikimedia CLI](https://gitlab.wikimedia.org/repos/releng/cli) | `wm`, formerly `mw` | Go | Wikimedia services and a MediaWiki development environment |
| [MediaWiki CLI](https://mwcli.toolforge.org/) by Sam Wilson | `mwcli` | PHP | any MediaWiki wiki: uploads, downloads, page exports |
| [wikibase-cli](https://codeberg.org/maxlath/wikibase-cli) | `wb` | JavaScript | Wikidata and other Wikibase instances |

Libraries and frameworks:

| Project | Language | Works with |
| --- | --- | --- |
| [Pywikibot](https://www.mediawiki.org/wiki/Manual:Pywikibot) | Python | any MediaWiki wiki: a bot framework with maintenance scripts (`pwb`) |
| [mwclient](https://github.com/mwclient/mwclient) | Python | the MediaWiki API |
| [mwcli on PyPI](https://pypi.org/project/mwcli/) | Python | helpers for MediaWiki command-line utilities, from mediawiki-utilities |

a Wikimedia CLI still installs `mw` while it moves to `wm`; if you use both, run that one as `wm`. Sam Wilson's MediaWiki CLI and the PyPI package share the name mwcli but are separate projects.
