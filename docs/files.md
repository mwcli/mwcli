# Files

## Viewing a file

`mw file view FILE` shows information about the latest version of an uploaded file: its URL, size, dimensions, MIME and media type, uploader and timestamp, summary, and repository when the file lives in a shared repository such as Wikimedia Commons. Public files need no login.

A file can be named with or without `File:` or the wiki's local name for that namespace (such as `파일:` on a Korean wiki). `--web` (`-w`) opens the file's description page in the browser; on a shared repository, that opens the description page on the repository wiki.

```sh
mw file view Example.jpg
mw file view 'File:Example.jpg' --json url,sha1
mw file view Example.jpg --wiki ko.wikipedia.org --web
```

`--json FIELDS` prints the named fields as JSON; `--jq` (`-q`) filters them. The fields follow the MediaWiki API (`prop=imageinfo`): `title`, `url`, `descriptionurl`, `size`, `width`, `height`, `mime`, `mediatype`, `timestamp`, `user`, `comment`, `sha1` and `repository`. Without a terminal, `mw file view` prints one `field:<TAB>value` line per field.

## Downloading files

`mw file download` saves the latest version of files into the directory of `-D`, `--dir` (the current directory by default). Name the files as arguments, give `-` to read file names from standard input one per line, or download the files in a category with `--category` (up to `-L`, `--limit`, 30 by default).

```sh
mw file download Example.jpg
mw file download --wiki commons --category 'Cats in snow' -L 10 --dir cats
mw page list --prefix 'File:Logo' | mw file download - --skip-existing
```

Each file is saved under its name without the `File:` prefix, with characters a file name cannot hold (`/ \ : * ? " < > |`, control characters, `%`, a leading dot, a final dot or space, and the first letter of a Windows reserved name) written as `%XX`. Each downloaded file is checked against the SHA-1 reported by the wiki before it is placed in the destination directory.

An existing file stops the command unless `--clobber` overwrites it or `--skip-existing` keeps it. Files that do not exist, unreadable files, and invalid file names are reported on standard error, and the command exits with status 1 after downloading the rest.
