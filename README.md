# mwcli

`mw` is MediaWiki on the command line. It logs in to wikis, reads and edits pages and makes Action API requests, in the style of the GitHub CLI (`gh`).

```sh
mw auth login --hostname mediawiki.org
mw auth status
mw page view 'Main Page'
mw page edit 'Sandbox' --summary 'Fix typo' --file page.wiki
mw api action=query meta=userinfo uiprop=rights
```

## Installation

```sh
go install github.com/zetaoss/mwcli/cmd/mw@latest
```

Or build from source with `make` (the binary is `bin/mw`) and install it with `make install` (`prefix=/usr/local` by default). Building requires Go 1.27 or newer. `mw --version` shows the version; releases are listed at <https://github.com/zetaoss/mwcli/releases>.

## Choosing a wiki

Commands that talk to a wiki take `-h, --hostname HOST`. Without it, the host is chosen in this order:

1. `MW_HOST`
2. the host of the last `mw auth login`
3. `en.wikipedia.org`

mw finds the host's `api.php` itself: it tries `https://HOST/w/api.php`, then `https://HOST/api.php`, then the `EditURI` link of the main page, follows redirects (so `mediawiki.org` reaches `www.mediawiki.org`) and checks that the answer comes from MediaWiki. Like the kubectl discovery cache, the result and the site information are kept in `discovery/HOST/siteinfo.json` in the cache directory (`$XDG_CACHE_HOME/mw`, else `%LocalAppData%\mw` on Windows, else `~/.cache/mw`) for 24 hours, and dropped when the API answers 404. Deleting the directory is always safe.

`--api URL` names the endpoint directly; `mw auth login --api URL` stores it in `hosts.yml` for that host, and then no discovery is done for it.

## Authentication

`mw auth login` prefers an owner-only OAuth 2 access token. Create one on the wiki at `Special:OAuthConsumerRegistration/propose/oauth2` ("This consumer is for use only by …") with the grants the tool needs, such as basic and editpage. Owner-only tokens do not expire; revoke them at `Special:OAuthManageMyGrants`. `--with-token` reads the token from standard input. `--username` logs in with a password through the `clientlogin` flow instead, read from `MW_PASSWORD` or a no-echo prompt; extra steps such as two-factor authentication are not supported.

Credentials are stored per host and account in `hosts.yml` in the configuration directory, with owner-only permissions. Treat this file as a credential. The directory is `MW_CONFIG_DIR`, else `$XDG_CONFIG_HOME/mw`, else `%AppData%\mw` on Windows, else `~/.config/mw`. A login saved by the earlier `mwtool` (`mwtool/profile.json`) is moved there on first use.

- `mw auth status` checks every stored account and shows its API, auth method, a masked token, which of edit, create, move, delete and protect it may do, and its rights.
- `mw auth switch` changes the active account of a host; `mw auth logout` removes one; `mw auth token` prints a token.
- `MW_TOKEN` uses that access token instead of stored credentials, for scripts and CI.

To check one page, including protection and blocks, ask the wiki with `mw api`. `"actions":{"edit":true}` means the edit would succeed; add `intestactionsdetail=full` to get the reasons when it would not:

```sh
mw api action=query prop=info intestactions=edit titles=Sandbox
```

## API requests

`mw api` sends `key=value` arguments (or `-f key=value`) to `api.php` and prints the response, indented on a terminal. `-F key=@file` reads a value from a file (`@-` for standard input). `format=json` and `formatversion=2` are added unless given. With `-X POST` and no `token`, a CSRF token is fetched and added.

## Exit status

- 0: success
- 1: the command failed, or the API reported an error
- 2: wrong usage
- 4: a login is required

See [docs/command.md](docs/command.md) for the command tree.
