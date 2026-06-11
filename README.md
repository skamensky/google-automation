# google-automation

A Go CLI for personal Google automation.

## Current commands

```bash
google-automation auth login
google-automation auth list
google-automation auth activate you@example.com
google-automation auth scopes
google-automation auth logout
google-automation calendar calendars list
google-automation calendar events create --summary "Dinner" --start "2026-06-10 20:00" --end "2026-06-10 21:00" --attendee person@example.com
google-automation calendar events list --today
google-automation calendar events search "Dinner" --from "2026-06-10" --to "2026-06-17"
google-automation calendar events get EVENT_ID
google-automation calendar events update EVENT_ID --field summary="New title"
google-automation calendar events delete EVENT_ID --yes
google-automation calendar freebusy --from "2026-06-10 09:00" --to "2026-06-10 17:00"
google-automation contacts list
google-automation contacts get people/c123
google-automation contacts search shmuel
google-automation contacts update people/c123 --field emailAddresses=person@example.com
google-automation contacts export --family --has-email --output family-contacts.json
google-automation docs create --title TITLE --text "..."
google-automation docs upload ./notes.txt --title TITLE --parent root
google-automation docs get DOC_ID
google-automation docs export DOC_ID --format pdf --output doc.pdf
google-automation docs append DOC_ID --text "..."
google-automation docs replace DOC_ID --find old --replace new
google-automation docs batch-update DOC_ID --request-file request.json
google-automation drive files search "tax"
google-automation drive files get FILE_ID
google-automation drive files download FILE_ID --output ./file.pdf
google-automation drive files upload ./file.pdf --parent root
google-automation drive files download-zip --file FILE_ID --folder FOLDER_ID --output bundle.zip
google-automation drive folders list --parent root
google-automation drive folders create --name NAME --parent root
google-automation drive folders tree --parent root --depth 3
google-automation drive folders download FOLDER_ID --output folder.zip
google-automation sheets create --title TITLE
google-automation sheets get SPREADSHEET_ID
google-automation sheets values get SPREADSHEET_ID --range "Sheet1!A1:D20"
google-automation sheets values update SPREADSHEET_ID --range "Sheet1!A1" --values-file values.json
google-automation sheets values append SPREADSHEET_ID --range "Sheet1!A:D" --values-file values.json
google-automation sheets values clear SPREADSHEET_ID --range "Sheet1!A1:D20"
google-automation sheets tabs list SPREADSHEET_ID
google-automation sheets batch-update SPREADSHEET_ID --request-file request.json
google-automation slides create --title TITLE
google-automation slides get PRESENTATION_ID
google-automation slides export PRESENTATION_ID --format pptx --output deck.pptx
google-automation slides batch-update PRESENTATION_ID --request-file request.json
google-automation tasks lists list
google-automation tasks lists create --title TITLE
google-automation tasks lists get TASKLIST_ID
google-automation tasks lists update TASKLIST_ID --field title="New title"
google-automation tasks lists delete TASKLIST_ID --yes
google-automation tasks list --tasklist @default
google-automation tasks get TASK_ID --tasklist TASKLIST_ID
google-automation tasks create --tasklist TASKLIST_ID --title TITLE --notes "..." --due 2026-06-20
google-automation tasks update TASK_ID --tasklist TASKLIST_ID --field title="New title" --field due=2026-06-21
google-automation tasks complete TASK_ID --tasklist TASKLIST_ID
google-automation tasks uncomplete TASK_ID --tasklist TASKLIST_ID
google-automation tasks move TASK_ID --tasklist TASKLIST_ID --parent PARENT_TASK_ID
google-automation tasks move TASK_ID --tasklist TASKLIST_ID --destination-tasklist OTHER_TASKLIST_ID
google-automation tasks delete TASK_ID --tasklist TASKLIST_ID --yes
google-automation tasks clear-completed --tasklist TASKLIST_ID --yes
google-automation gmail search "from:alice"
google-automation gmail get-message MESSAGE_ID --output message.json
google-automation gmail get-thread THREAD_ID --output thread.json
google-automation gmail attachments list MESSAGE_ID
google-automation gmail attachments download MESSAGE_ID --output ~/Downloads/gmail
google-automation gmail addresses export --output addresses.csv
```

## Credentials

The CLI reads OAuth client credentials from:

```text
~/.automation/google/client_secret.json
```

Override paths with flags or environment variables:

```bash
google-automation --credentials-file /path/to/client_secret.json --token-file /path/to/token.json contacts list
GOOGLE_AUTOMATION_CREDENTIALS_FILE=/path/to/client_secret.json google-automation contacts list
GOOGLE_AUTOMATION_TOKEN_FILE=/path/to/token.json google-automation contacts list
```

The OAuth token cache defaults to:

```text
~/.automation/google/token.json
```

Both credential and token files are ignored by git.

Multiple Google account logins are stored under:

```text
~/.automation/google/accounts
```

The active account is used by Google API commands unless `--token-file` is provided.

## Security

Users must provide their own Google OAuth client credentials. Do not commit OAuth
client secrets, access tokens, refresh tokens, account files, cache databases, or
other files from `~/.automation/google/`.

## Local cache

Commands can use a local SQLite cache where the Google API supports it. The global defaults are:

```text
--cache-dir ~/.automation/google/cache
--cache-ttl 2m
```

The SQLite file is:

```text
~/.automation/google/cache/cache.sqlite
```

Use `--cache-ttl 0` for no expiration. Use `--no-cache` to disable cache entirely.

Disable caching with:

```bash
google-automation --no-cache contacts search shmuel
```

## Build

```bash
go build ./...
```

## Examples

```bash
go run . auth login
go run . auth login --no-browser
go run . auth list
go run . auth activate you@example.com
go run . calendar calendars list
go run . calendar events create --summary "Dinner" --start "2026-06-10 20:00" --end "2026-06-10 21:00" --attendee person@example.com
go run . calendar events list --today
go run . calendar events search "Dinner" --from "2026-06-10" --to "2026-06-17"
go run . calendar events update EVENT_ID --field start="2026-06-10 20:30" --field end="2026-06-10 21:30"
go run . calendar freebusy --calendar-id primary --from "2026-06-10 09:00" --to "2026-06-10 17:00"
go run . contacts list --page-size 25
go run . contacts list --json
go run . contacts get people/c123 --json
go run . contacts search shmuel
go run . contacts search shmuel --fields names,emailAddresses
go run . contacts search shmuel --fields all
go run . contacts update people/c123 --field emailAddresses=home:person@example.com
go run . contacts update people/c123 --field emailAddresses=person@example.com --field phoneNumbers=mobile:+15551234567
go run . contacts export --label "Personal Family" --has-email --output family-contacts.json
go run . --no-cache contacts search shmuel
go run . docs create --title "Scratch Doc" --text "hello"
go run . docs upload ./notes.txt --title "Uploaded Notes"
go run . docs export DOC_ID --format docx --output doc.docx
go run . docs batch-update DOC_ID --request-file request.json
go run . drive files search "tax" --page-size 50
go run . drive files upload ./file.pdf --parent root
go run . drive folders list --parent root --page-size 50
go run . drive files update FILE_ID --field name="New name"
go run . drive folders move FOLDER_ID --parent DESTINATION_FOLDER_ID
go run . sheets values update SPREADSHEET_ID --range "Sheet1!A1" --values-file values.json
go run . sheets tabs add SPREADSHEET_ID --title "New Tab"
go run . sheets batch-update SPREADSHEET_ID --request-file request.json
go run . slides create --title "Scratch Deck"
go run . slides batch-update PRESENTATION_ID --request-file request.json
go run . tasks lists create --title "Scratch Tasks"
go run . tasks create --tasklist TASKLIST_ID --title "Follow up" --due 2026-06-20
go run . tasks create --tasklist TASKLIST_ID --title "Subtask" --parent PARENT_TASK_ID
go run . tasks list --tasklist TASKLIST_ID --show-completed --page-size 25
go run . tasks move TASK_ID --tasklist TASKLIST_ID --destination-tasklist OTHER_TASKLIST_ID
go run . gmail search "from:alice has:attachment"
go run . gmail list --label INBOX --limit 25
go run . gmail get-message MESSAGE_ID --format metadata --output message.json
go run . gmail get-thread THREAD_ID --format full --output thread.json
go run . gmail addresses export --workers 4 --output addresses.csv
```

Note: Google rejected the Keep API scope for this OAuth client with `invalid_scope`, so Keep is not included in the default login bundle.

## License

MIT
