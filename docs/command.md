# Command structure

```text
mw
├── auth
│   ├── login   [-h HOST] [--api URL] [--with-token | -u USER]
│   ├── logout  [-h HOST] [-u USER]
│   ├── status  [-h HOST] [-a] [-t]
│   ├── switch  [-h HOST] [-u USER]
│   └── token   [-h HOST] [-u USER]
├── page
│   ├── view    [-h HOST] [--api URL] TITLE
│   └── edit    [-h HOST] [--api URL] [-s SUMMARY] (--text WIKITEXT | -F FILE) TITLE
├── api         [-h HOST] [--api URL] [-X METHOD] [-f key=value] [-F key=@file] [key=value ...]
├── completion  SHELL
└── version
```

As in `gh`, `-h` is `--hostname`; help is `--help`. Every command prints its own help with `--help`.

## Environment

| Variable | Use |
| --- | --- |
| `MW_HOST` | Default host when `--hostname` is not given |
| `MW_TOKEN` | OAuth 2 access token used instead of stored credentials |
| `MW_PASSWORD` | Password for `mw auth login --username` without a prompt |
| `MW_CONFIG_DIR` | Configuration directory (default `~/.config/mw`) |

## hosts.yml

```yaml
mediawiki.org:
    user: Alice
    users:
        Alice:
            auth: oauth2
            oauth_token: ...
wiki.example.org:
    api: https://wiki.example.org/api.php
    user: Ann
    users:
        Ann:
            auth: session
            cookies:
                examplewiki_session: ...
```

`api` is stored only when it was given with `mw auth login --api` and differs from `https://HOST/w/api.php`; a discovered API stays in the cache. `user` is the active account. `config.yml` in the same directory holds `default_host`, set by the last login.

## Discovery cache

`~/.cache/mw/discovery/HOST/siteinfo.json` (under `$XDG_CACHE_HOME` when set) records what mw found for a host without a stored `api`, the default path included, for 24 hours:

```json
{
  "api": "https://www.mediawiki.org/w/api.php",
  "server": "//www.mediawiki.org",
  "script": "/w/index.php",
  "articlepath": "/wiki/$1",
  "generator": "MediaWiki 1.48.0-wmf.5",
  "sitename": "MediaWiki",
  "checked": "2026-11-21T10:00:00Z"
}
```
