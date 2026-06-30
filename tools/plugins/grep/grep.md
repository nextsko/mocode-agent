Search file contents by regex or literal text; returns matching file paths sorted by modification time (max 100); respects .gitignore. Use glob to filter by filename, not contents.

<usage>
- Provide regex pattern to search within file contents
- Set literal_text=true for exact text with special characters
- Optional starting directory (defaults to current working directory)
- Optional include pattern to filter which files to search
- Results sorted with most recently modified files first
</usage>

<limitations>
- Results limited to 100 files
- Performance depends on number of files searched
- Very large binary files may be skipped
- Hidden files (starting with '.') skipped
</limitations>
