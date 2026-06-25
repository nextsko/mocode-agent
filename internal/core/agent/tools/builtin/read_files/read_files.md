Reads multiple files concurrently with support for wildcards and path patterns.

<usage>
- Provide list of file paths to read
- Optional max_file_size: limit for individual files (default 5MB)
- Supports glob patterns (*, **, ?)
- Partial failures don't affect other files
</usage>

<features>
- Concurrent file reading for better performance
- Glob pattern support for path expansion
- Individual error handling per file
- File size protection mechanism
- Permission handling for files outside working directory
- Detailed error messages with suggestions
- Truncation indicator when file exceeds line limit
</features>

<limitations>
- Max file size per file: 5MB (configurable)
- Max files per request: 100
- Line limit: first 2000 lines per file (use view tool for more)
- Binary files (except images) may not display correctly
- Hidden files (starting with '.') are skipped
</limitations>

<cross_platform>
- Handles Windows (CRLF) and Unix (LF) line endings
- Works with forward slashes (/) and backslashes (\)
- Path separators normalized automatically
</cross_platform>

<tips>
- Use glob patterns to read multiple files: "**/*.go"
- Combine with specific paths: ["src/main.go", "tests/**/*_test.go"]
- For large directories, use more specific patterns
- Check individual file errors in response
</tips>

<examples>
Read specific files:
```json
{
  "paths": ["src/main.go", "src/utils.go", "README.md"]
}
```

Read with glob patterns:
```json
{
  "paths": ["**/*.go", "tests/**/*_test.go"]
}
```

Read with custom file size limit:
```json
{
  "paths": ["large_file.log"],
  "max_file_size": 10485760
}
```
</examples>

<error_handling>
- File not found: Returns error for that specific file, others succeed
- Permission denied: Returns error for that file
- File too large: Returns error with actual size and limit
- Invalid path: Returns error with suggestions for similar files
- Directory path: Returns error indicating path is a directory
</error_handling>
