# Choosing a wiki

A site is a MediaWiki wiki and its Action API endpoint. Every command that talks to a wiki takes `--wiki WIKI`, which accepts:

- a site you added, by name or alias, or a built-in site such as `commons`;
- a host, such as `wiki.example.org` (https);
- the URL of any page of the wiki, such as `https://wiki.example.org/wiki/Main_Page`, which also tells apart wikis that share a host under different paths;
- the URL of its `api.php`, used as it is.

`MW_WIKI` accepts the same. Without `--wiki`, commands that talk to a wiki use `MW_WIKI`, then the configured default site, then `en.wikipedia.org`. A successful login sets the default if it is unset. To see or change the default, use:

```sh
mw site default
mw site default commons
```

With no argument, `mw site default` shows the effective choice, including an override from `MW_WIKI`.

## Built-in and added sites

`mw site list --builtin` lists the built-in sites and their aliases, including `wikipedia`, `commons`, `wikidata` and `mw`. You can use them immediately:

```sh
mw page view --wiki commons 'Main Page'
```

## Wikis that were not added

For a host or page URL that is neither added nor built in, mw looks for the API once: at `/w/api.php`, then `/api.php` of a host, then through the `EditURI` link that MediaWiki puts in every page, which also finds an API at another path such as `/wiki/api.php`. A page URL is looked up through its `EditURI` link only, so that a wiki under a path is not mistaken for another one on the same host. mw keeps what it found in `apis.yml` in its cache directory, so later commands go straight to the API:

```sh
mw page view 'Main Page' --wiki wiki.example.org
mw page history 'Main Page' --wiki https://docs.example.org/handbook/wiki/Start
```

A wiki found from a page URL is kept under the prefix of its page URLs, such as `https://docs.example.org/handbook/wiki/`, so other pages of that wiki use the same entry. A URL with a query is kept as it is and matches only itself, since a query such as `?wiki=a` can pick the wiki. `mw site list --cached` lists the entries with the date each was found, and `mw site remove KEY` forgets one. When a cached API stops answering as MediaWiki, mw forgets it and says so; the next command looks again.

The cache holds only API addresses, never credentials: logins are kept for added sites. The cache directory is `MW_CACHE_DIR`, else `mw` in the user cache directory (`$XDG_CACHE_HOME` or `~/.cache` on Linux, `~/Library/Caches` on macOS, `%LocalAppData%` on Windows).

## Adding a site

Add a wiki to give it a short name, aliases or a farm:

```sh
mw site add wiki.example.org
mw site add docs https://wiki.example.org/docs/api.php --alias handbook
mw page view --wiki handbook 'Main Page'
```

`mw site add [NAME] WIKI` accepts a host, a page URL or an exact `api.php` URL. Given a host or page URL, it looks for the API at standard paths and checks the page's `EditURI` link if needed. Given an API URL, it checks that endpoint directly. A successful first login to an unregistered wiki also discovers and saves the site.

By default, each added site gets its own farm and login. To explicitly share credentials with an existing farm, pass `--farm`:

```sh
mw site add handbook https://docs.example.org/w/api.php --farm docs
```

`mw site list` shows one row per API endpoint. If several endpoints share a host, it uses an alias or the exact API URL to identify each row. `mw site remove NAME` accepts a listed name, alias or API URL; a host that matches multiple endpoints is rejected as ambiguous. Removing one wiki keeps farm logins while other wikis remain. To revoke a token or bot password, use the wiki's settings.


Built-in and custom wikis are organized under named farms in `config.yml`. A farm is the login-sharing scope. Every wiki entry in one farm uses the farm’s stored accounts. See [Commands](command.md) for the configuration shape.
