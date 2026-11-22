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
| `--prefix TEXT` | pages whose titles start with `TEXT` | `list=allpages` |
| `--category NAME` | members of the category, with or without `Category:`; a name that already starts with a name of the category namespace, local or an alias, is used as it is | `list=categorymembers` |
| `-S`, `--search QUERY` | full-text search results | `list=search` |
| `--links-to TITLE` | pages that link to `TITLE` | `list=backlinks` |
| `--embeds TITLE` | pages that transclude `TITLE`, such as a template | `list=embeddedin` |

`-n`, `--namespace ID` limits the list to one namespace; `-L`, `--limit` sets the most pages (30 by default), fetched across as many requests as needed. `--json` takes `title`, `pageid` and `ns` and prints an array; `--jq` filters it.

```sh
mw page list --category 'Living people' -L 100
mw page list --embeds 'Template:Infobox' --namespace 0 --json title --jq '.[].title'
```

## Editing pages

`mw page edit TITLE` replaces the wikitext from `--text` or `--file` (`-` for standard input).
