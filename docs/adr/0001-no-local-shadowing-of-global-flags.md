# ADR 0001: Do Not Shadow Global CLI Flags

## Status

Accepted

## Context

`google-automation` has global flags whose meaning should stay consistent everywhere. For example:

```bash
--json
```

means machine-readable JSON output globally.

The first version of:

```bash
docs batch-update
```

used a local `--json request.json` flag for the request body file. That shadowed the global `--json` output flag on this leaf command and made the CLI contract inconsistent.

## Decision

Subcommands must not define local flags that shadow global persistent flags.

Use specific local flag names instead. For Docs batch updates, the request body file flag is:

```bash
docs batch-update DOC_ID --request-file request.json
```

No stdin shortcut is supported for this command.

## Consequences

- Global flags keep one meaning across the CLI.
- Leaf commands are more explicit about local inputs.
- New commands must check global flag names before adding local flags.
- If a command needs a JSON input file, prefer names such as `--request-file`, `--body-file`, or another domain-specific flag instead of `--json`.
