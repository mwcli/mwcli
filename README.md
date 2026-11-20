# mwtool

`mwtool` is a command line client for reading and editing MediaWiki pages. The first version uses the MediaWiki Action API directly.

## Commands

```sh
mw auth status
mw auth login [--api https://zetawiki.com/w/api.php] [--username USER]
mw page get 'Page title'
mw page edit 'Page title' --summary 'Fix typo' --file page.wiki
mw page edit 'Page title' --summary 'Fix typo' --text 'Wikitext'
```

Build the executable as `mw` with `go build -o mw .`. `MWTOOL_API` and `MWTOOL_USERNAME` can provide defaults. The default API endpoint is ZetaWiki. `page get` can read public pages without logging in.

Login uses MediaWiki's `clientlogin` Action API flow. `mw auth status` checks the saved session against the wiki and reports the logged-in username. The session cookie jar is written to the user config directory under `mwtool/profile.json` with owner-only permissions. Treat this file as a credential. Password input uses a no-echo terminal prompt; set `MWTOOL_PASSWORD` for non-interactive login. Extra login steps such as two-factor authentication are not implemented yet.

Edits are submitted with an edit summary and the current CSRF token. Use `--file` for multiline page text.

## Development

Requires Go 1.27 or newer.

```sh
go build -o mw .
```
