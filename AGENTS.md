# Agent guidance

mw follows GitHub CLI (cli/cli). Before adding or changing a command, compare it with gh:
command and flag names, short flags, help text, output (TTY vs pipe, `--json`/`--jq`/`--web`),
errors and exit codes, config and environment variables. Keep the docs and code comments
about mw itself; do not explain features by reference to gh.

go.mod: before adding a module, check cli/cli's go.mod and prefer the module gh uses for the
same job (for example cobra, gojq, go-shellquote, heredoc). Add another only when gh has none,
and say why in the PR.
