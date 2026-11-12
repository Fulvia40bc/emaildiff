# Email List Diff (Windows ARM64)

A small native Windows GUI app (ARM64) for comparing two lists of email addresses.

## Features

- Three columns: **List A** (your emails), center column, **List B** (other emails)
- Center column shows either **"in B, not in A"** (missing from A — radio toggle) or **"only in A"**; header shows counts
- Drag & drop `.xlsx` / `.xlsm` / `.csv` / `.txt` files onto the left or right column (emails extracted from shared strings, rich-text runs, inline strings)
- `Load...` file pickers, **Paste** buttons and **Ctrl+V** to load from clipboard text, **Recent** files menu
- Normalization options: case-insensitive (default), strip `+tags`, domain-only comparison
- Duplicates within a list are flagged `(dup)` in the list and counted in the header
- Per-column filter boxes, **Sort A-Z**, select-and-remove entries, **Add to A** (moves selected center items into list A)
- Status bar with counts and **% match**
- Output: **Copy missing** / **Copy report** to clipboard, **Save report** as `.txt` or `.csv`, **Save/Load session** files

## Build (from any OS with Go 1.27+)

```sh
GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "-s -w -H windowsgui" -o EmailDiff-arm64.exe .
```

Runs on Windows 10/11 on ARM64 (e.g. Snapdragon / Windows 11 Copilot+ PCs).
Legacy binary `.xls` is not supported; save the workbook as `.xlsx` first.

## Layout

- `logic.go` — platform-independent: email extraction, xlsx parsing, normalization, diff, sessions
- `main.go` — Win32 GUI (Windows-only build tag)
