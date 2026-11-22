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

## Database lag

mw sends `maxlag=5` with every request, as Wikimedia asks of tools: while the wiki's database replicas lag more than 5 seconds, the wiki refuses the request before doing anything. mw then waits as long as the wiki's `Retry-After` says (5 seconds when it says nothing), prints a line on standard error, and sends the request again, up to 5 times; then the `maxlag` error is reported. Ctrl-C stops the wait. A wiki without replicas never refuses. With `mw api`, a `maxlag=N` parameter of your own replaces the default.

## REST API

A first argument that starts with `/` is a path of the wiki's REST API, under the `rest.php` next to its `api.php`, such as `https://www.mediawiki.org/w/rest.php/v1/page/MediaWiki/bare`. The path may have a query, and an escaped character such as `%2F` in a title stays as it is.

```sh
mw api /v1/page/Main_Page/bare --jq .latest.id
mw api '/v1/search/page?q=jq&limit=5' --jq '.pages[].title'
mw api -X POST /v1/page -f title=Sandbox/New -f source=Hello -f comment=test
mw api -X PUT /v1/page/Sandbox --input edit.json
```

`key=value` parameters go in the query of a `GET`, `HEAD` or `DELETE` request. For other methods they make a JSON object sent as the body; `-f` gives strings, and `-F` reads `@file` and turns `true`, `false`, `null` and integers into JSON values. `--input FILE` (`-` for standard input) sends the file as the body instead, as `application/json`, and the parameters then go in the query. A request that is not a `GET` and is sent with a login session, not OAuth, gets a `token` field with a CSRF token unless one is given, as the REST API needs for sessions; with `--input`, add it yourself.

The response is printed whatever its status, and a status other than 2xx exits with status 1. `--jq` needs a JSON response. `--paginate`, `--slurp` and `maxlag` are for the Action API only.
