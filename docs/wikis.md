# Choosing a wiki

Commands that talk to a wiki take `--wiki WIKI`: a host name such as `mediawiki.org` or an interwiki prefix such as `wikipedia`. Without it, the wiki is chosen in this order:

1. `MW_WIKI`
2. the host of the last `mw auth login`
3. `en.wikipedia.org`

mw finds the host's `api.php` itself: it tries `https://HOST/w/api.php`, then `https://HOST/api.php`, then the `EditURI` link of the main page, follows redirects (so `mediawiki.org` reaches `www.mediawiki.org`) and checks that the answer comes from MediaWiki. Like the kubectl discovery cache, the result and the site information are kept in `discovery/HOST/siteinfo.json` in the cache directory (`$XDG_CACHE_HOME/mw`, else `%LocalAppData%\mw` on Windows, else `~/.cache/mw`) for 24 hours, and dropped when the API answers 404. Deleting the directory is always safe.

Accounts are stored under the host of the API, so `mediawiki.org` and `www.mediawiki.org` are one wiki.

`--wiki` and `MW_WIKI` take the interwiki prefixes of MediaWiki's default [interwiki list](https://github.com/wikimedia/mediawiki/blob/master/maintenance/interwiki.list) that point to MediaWiki wikis, such as `wikipedia` (en.wikipedia.org), `commons`, `wikidata`, `mw` (www.mediawiki.org) and `metawikimedia`; `mw help interwiki` lists all 21. A name with a dot is always a host name.

```sh
mw page view --wiki commons 'Main Page'
```

`--api URL` names the endpoint directly; `mw auth login --api URL` stores it in `hosts.yml` for that host, and then no discovery is done for it.
