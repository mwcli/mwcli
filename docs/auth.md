# Authentication

`mw auth login` prefers an owner-only OAuth 2 access token. Create one on the wiki at `Special:OAuthConsumerRegistration/propose/oauth2` ("This consumer is for use only by …") with the grants the tool needs, such as basic and editpage. Owner-only tokens do not expire. To replace one that may have leaked, open `Special:OAuthConsumerRegistration/list` on the wiki where the consumer was registered, choose the consumer, tick "Reset the secret key to a new value" and submit: the wiki shows a new token and the old one stops working. On a wiki farm that is the central wiki, such as Meta-Wiki (`meta.wikimedia.org`) for Wikimedia projects like Commons. To log in again to the wiki you use with mw, run `mw auth login --wiki <host> --with-token` (or `--api <url>`); when a script or CI passes the token in `MW_TOKEN`, update that variable or secret instead. To stop using a consumer entirely, disable it on the same page. `--with-token` reads the token from standard input. `--username` logs in with a password through the `clientlogin` flow instead, read from `MW_PASSWORD` or a no-echo prompt; extra steps such as two-factor authentication are not supported.

Credentials are stored per host and account in `hosts.yml` in the configuration directory, with owner-only permissions. Treat this file as a credential. The directory is `MW_CONFIG_DIR`, else `$XDG_CONFIG_HOME/mw`, else `%AppData%\mw` on Windows, else `~/.config/mw`.

- `mw auth status` checks every stored account and shows its API, auth method, masked token, which of edit, create, move, delete and protect it may do, and its rights. `-t` shows the whole token.
  - The token shows only its last 4 characters; one of 8 characters or fewer is masked entirely.
  - MediaWiki's OAuth 2 tokens are JWTs. When the token carries the claims, its client (first 8 characters of `aud`), issue date (`iat`) and OAuth grants (`scopes`) are shown too. They are decoded for display only, not verified.
- `mw auth switch` changes the active account of a host; `mw auth logout` removes one; `mw auth token` prints a token in plain text, so keep it out of shared logs and terminals.
- `MW_TOKEN` uses that access token instead of stored credentials, for scripts and CI.

To check one page, including protection and blocks, ask the wiki with `mw api`. `"actions":{"edit":true}` means the edit would succeed; add `intestactionsdetail=full` to get the reasons when it would not:

```sh
mw api action=query prop=info intestactions=edit titles=Sandbox
```
