# Command structure

```text
mw
├── auth
│   ├── login   [--wiki WIKI] [--api URL] [--with-token | -u USER [--bot-password]]
│   ├── logout  [--wiki WIKI] [-u USER]
│   ├── status  [--wiki WIKI] [-a] [-t]
│   ├── switch  [--wiki WIKI] [-u USER]
│   └── token   [--wiki WIKI] [-u USER]
├── site
│   ├── add     [NAME] WIKI [--alias ALIAS]
│   ├── list    [--builtin]
│   ├── remove  NAME
│   └── default [NAME]
├── page
│   ├── view    [--wiki WIKI] [--api URL] [-w] [--json FIELDS [-q EXPR]] TITLE
│   ├── list    [--wiki WIKI] [--api URL] [--prefix TEXT | --category NAME | -S QUERY | --links-to TITLE | --embeds TITLE] [-n ID] [-L N] [--json FIELDS [-q EXPR]]
│   └── edit    [--wiki WIKI] [--api URL] [-s SUMMARY] (--text WIKITEXT | -F FILE) TITLE
├── api         [--wiki WIKI] [--api URL] [-X METHOD] [-f key=value] [-F key=@file] [key=value ...]
├── completion  SHELL
└── version
```

`--wiki WIKI` accepts a site name, an alias or a host. `--api URL` names an exact Action API endpoint. See [Choosing a wiki](wikis.md) for the target selection order. Every command prints its own help with `-h, --help`.

## Environment

| Variable | Use |
| --- | --- |
| `MW_WIKI` | Wiki override for requests that omit `--wiki` and `--api` |
| `MW_TOKEN` | OAuth 2 access token used instead of stored credentials |
| `MW_PASSWORD` | Password or bot password for `mw auth login --username` without a prompt |
| `MW_CONFIG_DIR` | Configuration directory (default `~/.config/mw`) |
| `MW_BROWSER` | Browser command for `--web`, before `BROWSER` |

## config.yml

```yaml
sites:
    default: docs
    wikis:
        docs:
            api: https://wiki.example.org/docs/api.php
            aliases:
                - handbook
logins:
    docs:
        user: Alice
        users:
            Alice:
                auth: oauth2
                oauth_token: ...
            Bob:
                auth: botpassword
                bot_name: Bob@mybot
                bot_password: ...
```

`sites.wikis` holds added sites. An entry can also include `server`, `script` and `articlepath` from the wiki's site information. `sites.default` names the configured default. `logins` holds accounts by site name, including accounts for built-in sites. Each site's `user` is its active account. Account-password logins use `auth: session` and store `cookies`. See [Authentication](auth.md) for credential storage details.

## Exit status

- 0: success
- 1: the command failed, or the API reported an error
- 2: wrong usage
- 4: a login is required
