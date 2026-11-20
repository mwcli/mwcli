# Command structure

```text
mw
├── auth
│   ├── status [--api URL]
│   ├── login [--api URL] [--username USER | --token]
│   └── can-i [--api URL] (ACTION [TITLE] | --list)
└── page
    ├── get [--api URL] TITLE
    └── edit [--api URL] [--summary TEXT] (--text WIKITEXT | --file PATH) TITLE
```

`auth status` checks the saved login session or token. `auth can-i` prints yes or no (exit 0 or 1) for edit, create, move, delete or protect, on a given page when a title is given. `auth login` prompts for credentials and saves the session; with `--token` it saves an OAuth 2 access token instead. `page get` prints page wikitext; `page edit` replaces it and accepts an optional edit summary.

`--api` selects a wiki's `api.php` endpoint. `MWTOOL_API` and `MWTOOL_USERNAME` provide defaults. `MWTOOL_PASSWORD` can supply the password and `MWTOOL_TOKEN` the access token for non-interactive login; otherwise the terminal prompts without echo.
