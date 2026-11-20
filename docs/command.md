# Command structure

```text
mw
├── auth
│   ├── status [--api URL]
│   └── login [--api URL] [--username USER]
└── page
    ├── get [--api URL] TITLE
    └── edit [--api URL] [--summary TEXT] (--text WIKITEXT | --file PATH) TITLE
```

`auth status` checks the saved login session. `auth login` prompts for credentials and saves the session. `page get` prints page wikitext; `page edit` replaces it and accepts an optional edit summary.

`--api` selects a wiki's `api.php` endpoint. `MWTOOL_API` and `MWTOOL_USERNAME` provide defaults. `MWTOOL_PASSWORD` can supply the password for non-interactive login; otherwise the terminal prompts without echo.
