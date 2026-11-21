# mwtool

`mwtool` is a command line client for reading and editing MediaWiki pages. The first version uses the MediaWiki Action API directly.

## Commands

```sh
mw auth status
mw auth login [--api https://en.wikipedia.org/w/api.php] [--username USER]
mw auth login --token [--api https://en.wikipedia.org/w/api.php]
mw auth can-i [--quiet] create 'Page title'
mw auth can-i --list
mw page get 'Page title'
mw page edit 'Page title' --summary 'Fix typo' --file page.wiki
mw page edit 'Page title' --summary 'Fix typo' --text 'Wikitext'
```

Build the executable as `mw` with `go build -o mw .`. `MWTOOL_API` and `MWTOOL_USERNAME` can provide defaults. The default API endpoint is English Wikipedia. OAuth access tokens are wiki-specific, so use `--api` or `MWTOOL_API` to select the wiki that issued the token. `page get` can read public pages without logging in.

Login uses MediaWiki's `clientlogin` Action API flow. `mw auth status` checks the saved session against the wiki and reports the logged-in username. The session cookie jar is written to the user config directory under `mwtool/profile.json` with owner-only permissions. Treat this file as a credential. Password input uses a no-echo terminal prompt; set `MWTOOL_PASSWORD` for non-interactive login. Extra login steps such as two-factor authentication are not implemented yet.

`mw auth login --token` uses an OAuth 2 owner-only access token instead of a password. Create one on the wiki (Special:OAuthConsumerRegistration/propose/oauth2, "This consumer is for use only by …") with the grants the tool needs, such as basic and editpage. The token is read with a no-echo prompt or from `MWTOOL_TOKEN`, checked against the wiki, and saved in `profile.json`. Requests then send it as `Authorization: Bearer`. When `MWTOOL_TOKEN` is set, it is used instead of the saved profile. Owner-only tokens do not expire; revoke them at Special:OAuthManageMyGrants.

`mw auth can-i ACTION [TITLE]` prints `yes` or `no` and exits 0 or 1, like `kubectl auth can-i`, so scripts can check before acting; `-q`/`--quiet` prints nothing. Unlike kubectl, a failed check (bad token, network or API error, wrong usage) exits 2, so it is never mistaken for `no`. ACTION is edit, create, move, delete or protect. With a title it asks the wiki whether that action would succeed on that page (`intestactions`), including protection and blocks, and prints the reasons when it would not. Without a title it only checks the matching user right. `--list` prints the rights of the current login; with a token these are already limited to its grants.

Edits are submitted with an edit summary and the current CSRF token. Use `--file` for multiline page text.

## Development

Requires Go 1.27 or newer.

```sh
go build -o mw .
```

## Exit status

- 0: success, or `yes` from `auth can-i`
- 1: the command failed (API error, not logged in, missing page), or `no` from `auth can-i`
- 2: wrong usage; for `auth can-i`, any failure to decide
