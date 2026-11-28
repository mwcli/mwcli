# Pages

## Viewing a page

`mw page view TITLE` prints the wikitext of the latest revision. `--follow-redirects` resolves redirects and prints their target page instead of the redirect itself; the output does not show which title it resolved to, so check `--json title` before editing rather than piping `mw page view TITLE --follow-redirects` straight back into `mw page edit TITLE`. `--web` (`-w`) opens the page in the browser instead (`MW_BROWSER`, else `BROWSER`, else the system opener). The browser setting is a program path, spaces allowed, or a command line with arguments where a path with spaces is quoted.

`mw browse [TITLE]` opens a page, or the main page with no title, without reading it first, so special pages such as `Special:RecentChanges` work too. `--no-browser` (`-n`) prints the URL instead of opening it:

```sh
mw browse 'Main Page' -n
```

`--json FIELDS` prints the named fields as JSON; `--json` alone lists them. Field names follow the MediaWiki API: `title`, `pageid`, `ns`, `url`, `contentmodel`, `length`, `touched`, `redirect`, `protection` (type, level, expiry), `categories` (every title, hidden ones included), `revision` (revid, parentid, timestamp, user, comment, size) and `content` (the wikitext, fetched only when asked for). `--jq` (`-q`) filters the JSON with a jq expression; strings print without quotes.

```sh
mw page view 'Main Page' --json pageid,url,revision
mw page view 'Main Page' --json categories --jq '.categories[]'
mw page view 'Alias' --follow-redirects
```

## Listing pages

`mw page list` prints page titles, one per line. Without a source it lists all pages in title order; choose at most one source:

| Flag | Lists | API module |
| --- | --- | --- |
| `--prefix TEXT` | pages whose titles start with `TEXT`; a `TEXT` that starts with a namespace name, local, canonical or an alias, such as `Help:Edit`, lists that namespace (a different `--namespace` is an error), and any other `TEXT` is in the main namespace or the one `--namespace` selects | `list=allpages` |
| `--category NAME` | members of the category, with or without `Category:`; a name that already starts with a name of the category namespace, local or an alias, is used as it is | `list=categorymembers` |
| `-S`, `--search QUERY` | full-text search results | `list=search` |
| `--links-to TITLE` | pages that link to `TITLE` | `list=backlinks` |
| `--embeds TITLE` | pages that transclude `TITLE`, such as a template | `list=embeddedin` |
| `--redirects-to TITLE` | pages that redirect to `TITLE` | `prop=redirects` |

`--filter-redirects all|redirects|nonredirects` (`all` by default) filters redirects with `--prefix`, `--links-to`, `--embeds` or no source. With `--links-to`, `--follow-redirects` also lists pages that link to `TITLE` through redirects.

`-n`, `--namespace ID` limits the list to one namespace; `-L`, `--limit` sets the most pages (30 by default), fetched across as many requests as needed. `--json` takes `title`, `pageid` and `ns` and prints an array; `--jq` filters it.

```sh
mw page list --category 'Living people' -L 100
mw page list --prefix 'Help:Edit'
mw page list --embeds 'Template:Infobox' --namespace 0 --json title --jq '.[].title'
mw page list --redirects-to 'Main Page'
mw page list --links-to 'Main Page' --follow-redirects --filter-redirects nonredirects
```

## Downloading pages

`mw page download` saves the latest revision of pages as files, asking the wiki for fifty pages a request. Name the pages as arguments, give `-` to read titles from standard input one per line, or choose them with the sources of `mw page list` (`--prefix`, `--category`, `--search`, `--links-to`, `--embeds`, `--redirects-to`, `--filter-redirects`, with `--namespace` and `-L`, 30 by default). `--follow-redirects` resolves redirects to their target pages when fetching content, and with `--links-to` also includes pages linking through redirects.

```sh
mw page download 'Main Page' 'Help:Contents'
mw page download 'Alias' --follow-redirects
mw page download --category 'Living people' -L 500 --dir people
mw user contribs Example --json title --jq '.[].title' | sort -u | mw page download -
```

Files go to the directory of `-D`, `--dir` (the current one by default), named after the title with an extension for the content model: `.wiki` for wikitext, `.js`, `.css`, `.json`, `.lua` for Scribunto modules, and `.txt` for any other; a title that already ends with it, such as `MediaWiki:Common.css`, gets none. `/ \ : * ? " < > |`, control characters and `%` itself are written as `%XX`, as are a leading dot, a final dot or space, and the first letter of a name Windows reserves such as `CON`, so `Help:Edit/ko` becomes `Help%3AEdit%2Fko.wiki` and the title can be read back from the name.

An existing file stops the command unless `--clobber` overwrites it or `--skip-existing` keeps it. Pages that do not exist and invalid titles are reported on standard error, and the command exits with status 1 after saving the rest.

## Page history

`mw page history TITLE` lists the revisions of a page, newest first: revision ID, time in UTC, user, size in bytes, `m` for a minor edit, and the edit summary. On a terminal the columns line up, counting Korean, Japanese and Chinese characters as two cells, and long summaries are shortened; in a pipe the columns are separated by tabs, with the full time in ISO 8601 and the full summary, its line breaks and tabs turned into spaces and other spacing kept.

`--follow-redirects` resolves redirects to their target page. `-L`, `--limit` sets the most revisions (30 by default), fetched across as many requests as needed. `--json` takes `revid`, `parentid`, `timestamp`, `user`, `comment`, `size`, `minor` and `tags` and prints an array; `--jq` filters it. A user or summary hidden by an administrator is empty.

```sh
mw page history 'Main Page' -L 5
mw page history 'Main Page' --json revid --jq '.[0].revid'
mw page history 'Alias' --follow-redirects
```

## Contributions of a user

`mw user contribs USER` lists the edits of a user or an IP address, newest first: time in UTC, page, change in bytes, marks and the edit summary. The marks are `N` for a new page, `m` for a minor edit and `*` when the edit is still the latest revision of its page. On a terminal the columns line up and long summaries are shortened; in a pipe each line is the revision ID, the time in ISO 8601, the title, the change in bytes, the marks and the full summary, separated by tabs. `--namespace` (`-n`) keeps one namespace, `-L` sets how many edits (30 by default), and `--web` opens the wiki's contributions page. A user with no edits, or who does not exist, lists nothing.

```sh
mw user contribs Example -L 100
mw user contribs Example --namespace 0 --json title --jq '.[].title' | sort -u
```

The JSON fields are named as in the MediaWiki API (`list=usercontribs`): `revid`, `parentid`, `pageid`, `ns`, `title`, `timestamp`, `user`, `comment`, `size`, `sizediff`, `minor`, `new`, `top` and `tags`.

## Comparing revisions

`mw page diff` prints the changes between two revisions of wikitext as a unified diff, which tools such as `patch` and editors understand. A number is a revision ID; anything else is a page title, meaning its latest revision. `--follow-redirects` resolves title arguments through redirects.

- `mw page diff A`: the changes made by revision `A`, or by the latest edit of page `A`, from the revision before it. A page's first revision is compared with empty text (`/dev/null`).
- `mw page diff A B`: from `A` to `B`, which may be revisions of different pages.

Each side is labeled with the title and, after a tab, the revision, its time and user. The texts are compared as they are: MediaWiki keeps no final newline, which the diff marks with `\ No newline at end of file`, so a change of that newline shows too. Identical revisions print nothing. On a terminal, removed lines are red and added lines green; `--color always|never|auto` overrides this, and `NO_COLOR` turns it off. `--web` opens the wiki's own diff page (`Special:Diff`) instead.

```sh
mw page diff 'Main Page'
mw page diff 1001 1042
mw page diff "$(mw page history 'Main Page' -L 10 --json revid --jq '.[9].revid')" 'Main Page'
```

## Creating and editing pages

`mw page edit TITLE` replaces the wikitext of an existing page from `--text` or `--file` (`-` for standard input). `mw page create TITLE` makes a new page the same way. Each refuses the other's job, so a typo in a title does not create a page and a new page does not overwrite one made meanwhile; the error says to run the same command with the other one.

- `-s`, `--summary` sets the edit summary.
- `--minor` marks the edit minor. `--bot` marks it a bot edit, which hides it from recent changes when the account has the bot right.
- When the text is the same as the page's, the wiki saves no revision and mw says `No changes`.
- `--baserevid ID` (`page edit`) names the revision the new text was made from, such as the `revid` of `mw page view --json revision`. When others edited the page since, the wiki merges their changes as the browser editor does, or the edit fails with an edit conflict instead of undoing them; later edits by the same account do not count. With `--dry-run`, mw says when the page changed since that revision.

  ```sh
  rev=$(mw page view 'Sandbox' --json revision --jq .revision.revid)
  mw page view 'Sandbox' > sandbox.wiki
  # edit sandbox.wiki
  mw page edit 'Sandbox' -F sandbox.wiki --baserevid "$rev"
  ```
- `--dry-run` prints what the command would change as a diff, in the format of `mw page diff`, and saves nothing. The new side is the text as the wiki would save it, which the wiki works out (`action=parse&onlypst`): signatures such as `~~~~`, `subst:` and trailing white space come out as in a real edit, as your account when you are logged in. The text is transformed in the page's content model, or for a new page the model the wiki gives its title, so a JSON page, for one, shows the JSON the wiki would store. It needs no login, and refuses a missing page for `edit` and an existing one for `create` as a real run would.

Creating a page needs the right to create pages; for a token or bot password, the createeditmovepage grant.

```sh
mw page view 'Sandbox' | sed 's/teh/the/' | mw page edit 'Sandbox' -F - -s 'Fix typo' --minor
mw page create 'Sandbox/Test' --text 'Hello' -s 'New test page'
```

## Moving and deleting pages

`mw page move TITLE NEW-TITLE` renames a page with its history and leaves a redirect at the old title. `--move-talk` moves its talk page too and `--move-subpages` its subpages, as far as the wiki allows; `--no-redirect` leaves no redirect, which needs the right to suppress redirects. A new title that is taken is an error.

`mw page delete TITLE` deletes a page with all its revisions; administrators can undelete it. On a terminal mw asks to type the title to confirm, and in a script `--yes` is needed.

Both take `--reason`, shown in the log, and need a login with the right: move (such as the `createeditmovepage` grant of a token) or delete (the `delete` grant).

```sh
mw page move 'Sandbox/Old' 'Sandbox/New' --reason 'Better name' --move-talk
mw page delete 'Sandbox/New' --reason 'Test done' --yes
```
