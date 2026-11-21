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
www.mediawiki.org:
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

`api` is stored only when it differs from `https://HOST/w/api.php`. `user` is the active account. `config.yml` in the same directory holds `default_host`, set by the last login.
