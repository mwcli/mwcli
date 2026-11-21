# mwcli

`mw` is MediaWiki on the command line. It logs in to wikis, reads and edits pages and makes Action API requests, in the style of the GitHub CLI (`gh`).

```sh
mw auth login --hostname zetawiki.com
mw auth status
mw page view 'Main Page'
mw page edit 'Sandbox' --summary 'Fix typo' --file page.wiki
mw auth can-i delete 'Sandbox'
mw api action=query meta=userinfo uiprop=rights
```

## Installation

```sh
go install github.com/zetaoss/mwcli/cmd/mw@latest
```

Or build from source with `make` (the binary is `bin/mw`) and install it with `make install` (`prefix=/usr/local` by default). Building requires Go 1.27 or newer. `mw --version` shows the version; releases are listed at <https://github.com/zetaoss/mwcli/releases>.

## Choosing a wiki

Commands that talk to a wiki take `-h, --hostname HOST` and use `https://HOST/w/api.php`. `--api URL` gives the endpoint of a wiki whose `api.php` is elsewhere; `mw auth login --api URL` remembers it for that host. Without either flag, the wiki is chosen in this order:

1. `MW_HOST`
2. the host of the last `mw auth login`
3. `en.wikipedia.org`

## Authentication

`mw auth login` prefers an owner-only OAuth 2 access token. Create one on the wiki at `Special:OAuthConsumerRegistration/propose/oauth2` ("This consumer is for use only by …") with the grants the tool needs, such as basic and editpage. Owner-only tokens do not expire; revoke them at `Special:OAuthManageMyGrants`. `--with-token` reads the token from standard input. `--username` logs in with a password through the `clientlogin` flow instead, read from `MW_PASSWORD` or a no-echo prompt; extra steps such as two-factor authentication are not supported.

Credentials are stored per host and account in `hosts.yml` in the configuration directory, with owner-only permissions. Treat this file as a credential. The directory is `MW_CONFIG_DIR`, else `$XDG_CONFIG_HOME/mw`, else `%AppData%\mw` on Windows, else `~/.config/mw`. A login saved by the earlier `mwtool` (`mwtool/profile.json`) is moved there on first use.

- `mw auth status` checks every stored account and shows its API, auth method, a masked token, which of edit, create, move, delete and protect it may do, and its rights.
- `mw auth switch` changes the active account of a host; `mw auth logout` removes one; `mw auth token` prints a token.
- `MW_TOKEN` uses that access token instead of stored credentials, for scripts and CI.

`mw auth can-i ACTION [TITLE]` prints `yes` or `no` and exits 0 or 1, like `kubectl auth can-i`, so scripts can check before acting; `-q` prints nothing. With a title it asks the wiki whether the action would succeed on that page (`intestactions`), including protection and blocks. A failed check exits 2, so it is never mistaken for `no`. `--list` prints the rights of the login.

## API requests

`mw api` sends `key=value` arguments (or `-f key=value`) to `api.php` and prints the response, indented on a terminal. `-F key=@file` reads a value from a file (`@-` for standard input). `format=json` and `formatversion=2` are added unless given. With `-X POST` and no `token`, a CSRF token is fetched and added.

## Exit status

- 0: success, or `yes` from `auth can-i`
- 1: the command failed, or `no` from `auth can-i`
- 2: wrong usage; for `auth can-i`, any failure to decide
- 4: a login is required

See [docs/command.md](docs/command.md) for the command tree.
