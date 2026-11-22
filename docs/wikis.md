# Choosing a wiki

A site is a MediaWiki wiki and its Action API endpoint. Use `--wiki WIKI` with a site name, an alias or a host. Commands that accept `--api URL` can target an exact Action API endpoint. If you give both flags, they must resolve to the same site.

With neither flag, commands that talk to a wiki use `MW_WIKI`, then the configured default site, then `en.wikipedia.org`. A successful login sets the default if it is unset. To see or change the default, use:

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

For an unregistered host, a regular request tries `https://HOST/w/api.php`. If the wiki uses another path, add it first:

```sh
mw site add wiki.example.org
mw site add docs https://wiki.example.org/docs/api.php --alias handbook
mw page view --wiki handbook 'Main Page'
```

`mw site add [NAME] WIKI` accepts a host, a page URL or an exact `api.php` URL. Given a host or page URL, it looks for the API at standard paths and checks the page's `EditURI` link if needed. Given an API URL, it checks that endpoint directly. A successful first login to an unregistered wiki also discovers and saves the site. Use a separate name for each wiki when several wikis share a host.

`mw site list` shows added sites and marks the configured default with `*` if it appears in the list. `mw site remove NAME` removes an added site and its local logins. To revoke a token or bot password, use the wiki's settings.
