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

`mw site add [NAME] WIKI` accepts a host, a page URL or an exact `api.php` URL. Given a host or page URL, it looks for the API at standard paths and checks the page's `EditURI` link if needed. Given an API URL, it checks that endpoint directly. A successful first login to an unregistered wiki also discovers and saves the site.

By default, each added site gets its own farm and login. To explicitly share credentials with an existing farm, pass `--farm`:

```sh
mw site add handbook https://docs.example.org/w/api.php --farm docs
```

`mw site list` shows one row per API endpoint. If several endpoints share a host, it uses an alias or the exact API URL to identify each row. `mw site remove NAME` accepts a listed name, alias or API URL; a host that matches multiple endpoints is rejected as ambiguous. Removing one wiki keeps farm logins while other wikis remain. To revoke a token or bot password, use the wiki's settings.


Built-in and custom wikis are organized under named farms in `config.yml`. A farm is the login-sharing scope. Every wiki entry in one farm uses the farm’s stored accounts. See [Commands](command.md) for the configuration shape.
