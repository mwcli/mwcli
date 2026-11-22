# Authentication

mwcli recommends an OAuth 2 owner-only token where the wiki has the OAuth extension, and a bot password where it does not. Both limit mw to the grants you choose. An OAuth 1.0a owner-only consumer also works, for wikis or tools that offer only OAuth 1.0a.

| Method | Works on | Access | Revoke |
| --- | --- | --- | --- |
| OAuth 2 owner-only token | wikis with the OAuth extension, such as all Wikimedia wikis | the grants you chose | reset or disable the consumer |
| OAuth 1.0a owner-only consumer | wikis with the OAuth extension | the grants you chose | reset or disable the consumer |
| Bot password | most wikis (on by default) | the grants you chose | delete it at `Special:BotPasswords` |
| Account password | any wiki | all rights of your account | log out, or change the password |

> Treat tokens, consumer secrets and bot passwords like passwords. mw keeps them in `config.yml`. For account-password logins, mw saves only the session cookies.

Run `mw auth login` without flags to choose interactively.

## Keeping credentials secure

- Give a token or bot password only the grants your work needs, and replace it from time to time: these credentials do not expire.
- Keep the output of `mw auth token` and `mw auth status --show-token` out of chats, issues and logs.
- In scripts and CI, pass a token in `MW_TOKEN` from a secret store when you want to avoid saving credentials on disk.

mw stores credentials in plaintext per farm and account in `config.yml` in the configuration directory. On Unix, mw writes the file with mode `0600`. If you are upgrading from a version that used `hosts.yml`, sign in again to populate `config.yml`. The directory is `MW_CONFIG_DIR`, else `$XDG_CONFIG_HOME/mw`, else `%AppData%\mw` on Windows, else `~/.config/mw`.

## OAuth 2 owner-only token

**Create**: on the wiki, open `Special:OAuthConsumerRegistration/propose/oauth2`, tick "This consumer is for use only by …" and choose the grants, such as basic and editpage. On a wiki farm, register on the central wiki, such as Meta-Wiki (`meta.wikimedia.org`) for Wikimedia projects like Commons. The wiki shows the access token once.

**Log in**: `mw auth login --wiki <site> --with-token` reads the token from standard input; without `--with-token`, mw asks for it.

**Replace**: open `Special:OAuthConsumerRegistration/list` on the wiki where the consumer was registered, choose the consumer, tick "Reset the secret key to a new value" and submit: the wiki shows a new token and the old one stops working. To log in again to the wiki you use with mw, run `mw auth login --wiki <site> --with-token`; when a script or CI passes the token in `MW_TOKEN`, update that variable or secret instead.

**Revoke**: disable the consumer on the same page.

## OAuth 1.0a owner-only consumer

**Create**: on the wiki, open `Special:OAuthConsumerRegistration/propose/oauth1a`, tick "This consumer is for use only by …" and choose the grants. On a wiki farm, register on the central wiki, as for OAuth 2. The wiki shows four values once: the consumer token, consumer secret, access token and access secret.

**Log in**: `mw auth login --wiki <site> --oauth1` asks for the four values, the secrets without echo. With `--with-token`, mw reads them from standard input instead, in that order and separated by spaces or new lines. mw stores the four values and signs every request with them (HMAC-SHA1), so nothing expires. When the wiki rejects the keys, mw adds what the wiki found and which value to check, such as a consumer token copied in part; other OAuth errors, such as an expired timestamp, are shown as the wiki gives them.

**Replace and revoke**: as for OAuth 2, at `Special:OAuthConsumerRegistration/list` on the wiki where the consumer was registered.

## Bot password

**Create**: on the wiki, open `Special:BotPasswords`, give the bot password a name such as `mybot` and choose the grants. The wiki shows the login name, such as `Alice@mybot`, and the password once.

**Log in**: `mw auth login --wiki <site> --username Alice@mybot` reads the password from `MW_PASSWORD` or a no-echo prompt. On a wiki that separates the user and bot names with something other than `@` (`$wgUserrightsInterwikiDelimiter`), add `--bot-password`. mw stores the bot password and logs in with it again on each run, since a bot password session is short lived. When several wikis share a host, add each with its own site name so that each keeps its own credentials.

**Revoke**: delete or reset it at `Special:BotPasswords`.

## Account password

**Log in**: `mw auth login --wiki <site> --username Alice` reads the password from `MW_PASSWORD` or a no-echo prompt and logs in through `clientlogin`, asking the wiki to keep the session. mw stores only the session cookies. Extra steps such as two-factor authentication are not supported.

**End**: when the session expires, `mw auth status` says so; log in again. `mw auth logout` forgets the session.

## Checking what a login can do

- `mw auth status` checks every stored account and shows its API, auth method, masked token or bot password (for OAuth 1.0a, the consumer token and the masked access token), which of edit, create, move, delete and protect it may do, and its rights. `-t` shows the whole token or password.
  - A token or password shows only its last 4 characters; one of 8 characters or fewer is masked entirely.
  - MediaWiki's OAuth 2 tokens are JWTs. When the token carries the claims, its client (first 8 characters of `aud`), issue date (`iat`) and OAuth grants (`scopes`) are shown too. They are decoded for display only, not verified.
- `mw auth switch` changes the active account of a site; `mw auth logout` removes one; `mw auth token` prints an OAuth 2 token.
- `MW_TOKEN` uses that access token instead of stored credentials, for scripts and CI.

To check one page, including protection and blocks, ask the wiki with `mw api`. `"actions":{"edit":true}` means the edit would succeed; add `intestactionsdetail=full` to get the reasons when it would not:

```sh
mw api action=query prop=info intestactions=edit titles=Sandbox
```
