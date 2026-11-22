# Pages

`mw page view TITLE` prints the wikitext of the latest revision. `--web` (`-w`) opens the page in the browser instead (`MW_BROWSER`, else `BROWSER`, else the system opener). The browser setting is a program path, spaces allowed, or a command line with arguments where a path with spaces is quoted.

`--json FIELDS` prints the named fields as JSON; `--json` alone lists them. Field names follow the MediaWiki API: `title`, `pageid`, `ns`, `url`, `contentmodel`, `length`, `touched`, `redirect`, `protection` (type, level, expiry), `categories` (every title, hidden ones included), `revision` (revid, parentid, timestamp, user, comment, size) and `content` (the wikitext, fetched only when asked for). `--jq` (`-q`) filters the JSON with a jq expression; strings print without quotes.

```sh
mw page view 'Main Page' --json pageid,url,revision
mw page view 'Main Page' --json categories --jq '.categories[]'
```

`mw page edit TITLE` replaces the wikitext from `--text` or `--file` (`-` for standard input).
