# API requests

`mw api` sends `key=value` arguments (or `-f key=value`) to `api.php` and prints the response, indented on a terminal. `-F key=@file` reads a value from a file (`@-` for standard input). `format=json` and `formatversion=2` are added unless given. With `-X POST` and no `token`, a CSRF token is fetched and added.

## Following continue

A query that has more results than one response holds returns a `continue` object. `--paginate` sends the request again with those values, as MediaWiki asks (the original parameters plus the last `continue` values, which replace values of the same name, also in the query of a `--wiki` API URL), until a response has none, and prints each response. `--slurp` prints all the responses as one JSON array instead.

`--jq EXPR` (`-q`) filters each response with a jq expression; strings print as plain lines, and integers stay exact however large. It cannot be combined with `--slurp`. `--paginate` and `--jq` need `format=json`. When a response reports an error, mw prints it and stops with exit status 1.

```sh
# every member of a category, one title per line
mw api action=query list=categorymembers cmtitle=Category:Physics cmlimit=max --paginate --jq '.query.categorymembers[].title'

# all revisions of a page as one array
mw api action=query prop=revisions titles=Sandbox rvprop='ids|timestamp|user' rvlimit=max --paginate --slurp
```
