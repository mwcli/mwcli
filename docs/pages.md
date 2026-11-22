# Pages

## Viewing a page

`mw page view TITLE` prints the wikitext of the latest revision. `--web` (`-w`) opens the page in the browser instead (`MW_BROWSER`, else `BROWSER`, else the system opener). The browser setting is a program path, spaces allowed, or a command line with arguments where a path with spaces is quoted.

`--json FIELDS` prints the named fields as JSON; `--json` alone lists them. Field names follow the MediaWiki API: `title`, `pageid`, `ns`, `url`, `contentmodel`, `length`, `touched`, `redirect`, `protection` (type, level, expiry), `categories` (every title, hidden ones included), `revision` (revid, parentid, timestamp, user, comment, size) and `content` (the wikitext, fetched only when asked for). `--jq` (`-q`) filters the JSON with a jq expression; strings print without quotes.

```sh
mw page view 'Main Page' --json pageid,url,revision
mw page view 'Main Page' --json categories --jq '.categories[]'
```

## Listing pages

`mw page list` prints page titles, one per line. Without a source it lists all pages in title order; choose at most one source:

| Flag | Lists | API module |
| --- | --- | --- |
| `--prefix TEXT` | pages whose titles start with `TEXT`, without the namespace; use `--namespace` to select another namespace | `list=allpages` |
| `--category NAME` | members of the category, with or without `Category:`; a name that already starts with a name of the category namespace, local or an alias, is used as it is | `list=categorymembers` |
| `-S`, `--search QUERY` | full-text search results | `list=search` |
| `--links-to TITLE` | pages that link to `TITLE` | `list=backlinks` |
| `--embeds TITLE` | pages that transclude `TITLE`, such as a template | `list=embeddedin` |

`-n`, `--namespace ID` limits the list to one namespace; `-L`, `--limit` sets the most pages (30 by default), fetched across as many requests as needed. `--json` takes `title`, `pageid` and `ns` and prints an array; `--jq` filters it.

```sh
mw page list --category 'Living people' -L 100
mw page list --embeds 'Template:Infobox' --namespace 0 --json title --jq '.[].title'
```

## Page history

`mw page history TITLE` lists the revisions of a page, newest first: revision ID, time in UTC, user, size in bytes, `m` for a minor edit, and the edit summary. On a terminal the columns line up, counting Korean, Japanese and Chinese characters as two cells, and long summaries are shortened; in a pipe the columns are separated by tabs, with the full time in ISO 8601 and the full summary, its line breaks and tabs turned into spaces and other spacing kept.

`-L`, `--limit` sets the most revisions (30 by default), fetched across as many requests as needed. `--json` takes `revid`, `parentid`, `timestamp`, `user`, `comment`, `size`, `minor` and `tags` and prints an array; `--jq` filters it. A user or summary hidden by an administrator is empty.

```sh
mw page history 'Main Page' -L 5
mw page history 'Main Page' --json revid --jq '.[0].revid'
```

## Creating and editing pages

`mw page edit TITLE` replaces the wikitext of an existing page from `--text` or `--file` (`-` for standard input). `mw page create TITLE` makes a new page the same way. Each refuses the other's job, so a typo in a title does not create a page and a new page does not overwrite one made meanwhile; the error says to run the same command with the other one.

- `-s`, `--summary` sets the edit summary.
- `--minor` marks the edit minor. `--bot` marks it a bot edit, which hides it from recent changes when the account has the bot right.
- When the text is the same as the page's, the wiki saves no revision and mw says `No changes`.

Creating a page needs the right to create pages; for a token or bot password, the createeditmovepage grant.

```sh
mw page view 'Sandbox' | sed 's/teh/the/' | mw page edit 'Sandbox' -F - -s 'Fix typo' --minor
mw page create 'Sandbox/Test' --text 'Hello' -s 'New test page'
```
