# Getting started

This guide takes you from installing mw to editing a page on your wiki. It uses `wiki.example.org` as the wiki; put your wiki's host name in its place, such as `en.wikipedia.org` or the address of your own wiki.

## 1. Install mw

On Windows:

```sh
winget install mwcli.mwcli
```

On macOS and Linux, download an archive from the [releases](https://github.com/mwcli/mwcli/releases) page, or install with Go:

```sh
go install github.com/mwcli/mwcli/cmd/mw@latest
```

Check that it runs:

```sh
mw --version
```

## 2. Connect to your wiki

Use a built-in site such as `wikipedia`, or give your wiki's host name or the URL of any of its pages. The first time, mw finds the wiki's API and remembers it.

```sh
mw page view 'Main Page' --wiki wiki.example.org
```

If this prints the wikitext of the main page, mw can reach the wiki. Reading public pages needs no login. If mw cannot find the API from the host, give the URL of a page of the wiki, or of its `api.php`:

```sh
mw page view 'Main Page' --wiki https://wiki.example.org/path/index.php?title=Main_Page
```

See [Choosing a wiki](wikis.md) for more ways to add a site.

## 3. Get a credential

Choose one of these on the wiki, in this order of preference. [Authentication](auth.md) compares them.

1. **OAuth 2 owner-only token**, if the wiki has the OAuth extension (all Wikimedia wikis do): open `Special:OAuthConsumerRegistration/propose/oauth2` on the wiki, or on the central wiki of a wiki farm, which for Wikimedia projects such as Wikipedia and Commons is Meta-Wiki (`https://meta.wikimedia.org/wiki/Special:OAuthConsumerRegistration/propose/oauth2`). Tick "This consumer is for use only by …", choose the grants (basic and editpage are enough to edit pages) and submit. Copy the access token the wiki shows.
2. **OAuth 1.0a owner-only consumer**, if the wiki's OAuth extension offers only OAuth 1.0a, or you already have one: open `Special:OAuthConsumerRegistration/propose/oauth1a`, tick "This consumer is for use only by …", choose the grants and submit. Note the four values the wiki shows: consumer token, consumer secret, access token and access secret.
3. **Bot password**, if the wiki has no OAuth: open `Special:BotPasswords`, enter a name such as `mybot`, choose the grants and create it. Note the login name, such as `Alice@mybot`, and the password.
4. **Your account password**, only when none of these is available.

The wiki shows a token, consumer secret or bot password only once. Keep it to yourself: do not paste it into chats or issues.

## 4. Log in

```sh
mw auth login --wiki wiki.example.org
```

mw asks how to log in:

```text
? How would you like to authenticate mw?
  1. Paste an OAuth 2 access token
  2. OAuth 1.0a consumer keys
  3. Bot password (from Special:BotPasswords)
  4. Username and password
Choose [1]:
```

Choose the credential you got in step 3, then paste it or type it; nothing is shown as you type. mw checks it with the wiki and stores it:

```text
✓ Logged in to wiki.example.org as Alice
```

A successful first login adds the wiki if needed and makes it the default if you have yet to set one. To follow the examples below without repeating `--wiki`, set your default explicitly:

```sh
mw site default wiki.example.org
```

## 5. See what you may do

```sh
mw auth status
```

```text
wiki.example.org
  ✓ Logged in to wiki.example.org account Alice (…/mw/config.yml)
  - Active account: true
  - API: https://wiki.example.org/w/api.php
  - Auth: OAuth 2 access token
  - Token: ********************************a1B2 (client 0c1d2e3f, issued 2026-11-22)
  - Grants: basic, editpage
  - Can: edit · Cannot: create, move, delete, protect
  - Rights: …
```

`Can` lists what the login may do on the wiki. If something you need is under `Cannot`, create a credential with more grants and log in again.

## 6. Edit a page

Try it on a sandbox page first. Save its wikitext to a file, change the file, and save it back:

```sh
mw page view 'Sandbox' > sandbox.wiki
# edit sandbox.wiki in any editor
mw page edit 'Sandbox' --file sandbox.wiki --summary 'Testing mw'
```

```text
✓ Updated Sandbox on wiki.example.org
```

If the wiki has no page named Sandbox, `mw page view` says the page does not exist; write the file from scratch and make the page with `mw page create 'Sandbox' --file sandbox.wiki` instead. Creating a page needs the grant to create pages (createeditmovepage) as well as editpage.

Open the page in the browser to check the result:

```sh
mw page view 'Sandbox' --web
```

## 7. Use more than one wiki

Use `mw site list --builtin` to find built-in sites and `mw site list` to see sites you added. Log in separately on each site where you need to edit. Use `--wiki` for one command or `mw site default` to change the default. `MW_WIKI` takes precedence over the configured default.

```sh
mw auth login --wiki other.example.org
mw site default other.example.org
mw page view 'Main Page' --wiki wiki.example.org
MW_WIKI=other.example.org mw page list --namespace 12   # the Help: namespace
```

`mw auth status` lists your stored accounts. Use `mw auth switch --wiki wiki.example.org` to change the active account on that site, or `mw auth logout --wiki wiki.example.org` to remove an account from mw.

## Next

- [Pages](pages.md): view pages as JSON, list pages by category or search, edit from standard input
- [API requests](api.md): call any Action API module with `mw api`
- [Commands](command.md): every command and flag
