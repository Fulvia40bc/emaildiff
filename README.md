# Email List Diff (Windows ARM64)

A small native Windows GUI app (ARM64) for comparing two lists of email addresses.

## Features

- Three columns: **List A** (your emails), center (**missing from A / only in B**), **List B** (other emails)
- Drag & drop `.xlsx` / `.xlsm` / `.csv` / `.txt` files onto the left or right column (emails are extracted automatically, including from Excel shared strings and inline strings)
- `Load A...` / `Load B...` file-picker buttons as an alternative to drag & drop
- `Compare` recomputes the center column (case-insensitive matching, duplicates removed)
- `Copy missing` copies the center column to the clipboard; `Copy report` copies all three sections
- Status bar shows counts: A, B, common, only-in-A, missing-from-A

## Build (from any OS with Go 1.27+)

```sh
GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "-s -w -H windowsgui" -o EmailDiff-arm64.exe .
```

Runs on Windows 10/11 on ARM64 (e.g. Snapdragon / Windows 11 Copilot+ PCs).
Legacy binary `.xls` is not supported; save the workbook as `.xlsx` first.
