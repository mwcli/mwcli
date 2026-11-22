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

See [AGENTS.md](AGENTS.md) for how changes are designed and [Releasing](docs/releasing.md) for how versions are published.

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

Each release has a `checksums.txt` with the SHA-256 of its archives. After downloading an archive into the same directory:

```sh
sha256sum --check --ignore-missing checksums.txt
```

## Similar projects

- [a Wikimedia CLI](https://gitlab.wikimedia.org/repos/releng/cli) (`wm`, formerly `mw`): Wikimedia services and a MediaWiki development environment. It was called MediaWiki CLI before; if you use both, run it as `wm`.
- [MediaWiki CLI](https://mwcli.toolforge.org/) by Sam Wilson (`mwcli`, PHP): uploads, downloads and page exports. A separate project with the same short name.
- [Pywikibot](https://www.mediawiki.org/wiki/Manual:Pywikibot) (`pwb`, Python): a bot framework with many maintenance scripts.
- [mwclient](https://github.com/mwclient/mwclient) (Python): a MediaWiki API library.
- [wikibase-cli](https://codeberg.org/maxlath/wikibase-cli) (`wb`, JavaScript): Wikidata and other Wikibase instances.
- [mwcli on PyPI](https://pypi.org/project/mwcli/) (Python): helpers for building MediaWiki command-line utilities, from mediawiki-utilities.

mwcli is a single Go binary, `mw`, for reading and editing any MediaWiki wiki.
