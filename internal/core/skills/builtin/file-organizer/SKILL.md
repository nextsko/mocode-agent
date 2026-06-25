---
name: file-organizer
description: >-
  Use when the user needs to organize, sort, classify, clean up, or manage
  files in a directory. Covers scanning directories, categorizing files by
  type/date/size, moving files into structured folders, deduplication,
  renaming batches, cleaning temp files, and generating organization reports.
  Handles safety checks to avoid system directories.
---

# File Organizer

Organize, classify, and clean up files in directories.

## Core Workflow

1. **Scan** — List all files in source directory with metadata (size, date, type)
2. **Analyze** — Classify files by type, date, size, or custom rules
3. **Plan** — Generate organization blueprint (which files go where)
4. **Execute** — Move/copy/rename files after user approval
5. **Verify** — Confirm results and provide undo capability

## Safety Rules

- NEVER organize system directories (Windows, Program Files, System32, etc.)
- NEVER modify file contents — only move/copy/rename
- Always preview the plan before executing
- Skip shortcuts (.lnk, .url) and system files
- Block C:\ root and common install directories
- Provide undo capability for all operations

## Scanning Files

```python
import os
import datetime

def scan_directory(path, recursive=True):
    results = []
    for root, dirs, files in os.walk(path):
        for f in files:
            fp = os.path.join(root, f)
            stat = os.stat(fp)
            results.append({
                'path': fp,
                'name': f,
                'ext': os.path.splitext(f)[1].lower(),
                'size_kb': round(stat.st_size / 1024, 1),
                'modified': datetime.datetime.fromtimestamp(stat.st_mtime),
                'created': datetime.datetime.fromtimestamp(stat.st_ctime),
            })
        if not recursive:
            break
    return sorted(results, key=lambda x: x['ext'])
```

## Classification Strategies

### By File Type

```python
CATEGORIES = {
    'Documents':   ['.pdf', '.docx', '.doc', '.xlsx', '.xls', '.pptx', '.txt', '.md', '.csv'],
    'Images':      ['.jpg', '.jpeg', '.png', '.gif', '.bmp', '.svg', '.webp', '.ico', '.heic'],
    'Videos':      ['.mp4', '.avi', '.mkv', '.mov', '.wmv', '.flv', '.webm'],
    'Audio':       ['.mp3', '.wav', '.flac', '.aac', '.ogg', '.wma', '.m4a'],
    'Archives':    ['.zip', '.rar', '.7z', '.tar', '.gz', '.bz2', '.xz'],
    'Code':        ['.py', '.js', '.ts', '.go', '.rs', '.java', '.cpp', '.h', '.css', '.html'],
    'Data':        ['.json', '.xml', '.yaml', '.yml', '.sql', '.db', '.sqlite'],
    'Executables': ['.exe', '.msi', '.bat', '.sh', '.ps1', '.cmd'],
}
```

### By Date

```python
# Organize into Year/Month structure
# Downloads/2025/01-Jan/, Downloads/2025/02-Feb/, ...

def date_folder_name(dt):
    return f"{dt.year}/{dt.month:02d}-{dt.strftime('%b')}"
```

### By Size

```python
def size_category(size_kb):
    if size_kb < 100:
        return "tiny"         # < 100 KB
    elif size_kb < 1024:
        return "small"        # 100 KB - 1 MB
    elif size_kb < 10240:
        return "medium"       # 1 MB - 10 MB
    elif size_kb < 102400:
        return "large"        # 10 MB - 100 MB
    else:
        return "huge"         # > 100 MB
```

## Preview Plan

Before moving files, generate and show a JSON plan:

```python
import json

plan = {
    "source": "C:/Users/me/Downloads",
    "total_files": 150,
    "total_size_mb": 320.5,
    "categories": {
        "Documents": {"count": 30, "size_mb": 45.2},
        "Images":    {"count": 45, "size_mb": 120.8},
        "Archives":  {"count": 12, "size_mb": 80.3},
        "Other":     {"count": 63, "size_mb": 74.2},
    },
    "moves": [
        {"from": "report.pdf",      "to": "Documents/report.pdf"},
        {"from": "photo.jpg",       "to": "Images/photo.jpg"},
        {"from": "project.zip",     "to": "Archives/project.zip"},
    ]
}

print(json.dumps(plan, indent=2))
```

## Execute & Undo

```python
import json, shutil

# Save undo map before executing
undo_map = {}
for move in moves:
    src = os.path.join(source, move["from"])
    dst = os.path.join(target, move["to"])
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    undo_map[dst] = src
    shutil.move(src, dst)

# Save undo info
with open(os.path.join(target, ".organizer_undo.json"), "w") as f:
    json.dump(undo_map, f, indent=2)

# To undo:
# with open(".organizer_undo.json") as f:
#     undo = json.load(f)
# for dst, src in undo.items():
#     shutil.move(dst, src)
```

## Deduplication

Find and handle duplicate files:

```python
import hashlib

def file_hash(filepath):
    h = hashlib.md5()
    with open(filepath, "rb") as f:
        for chunk in iter(lambda: f.read(8192), b""):
            h.update(chunk)
    return h.hexdigest()

def find_duplicates(files):
    by_hash = {}
    for f in files:
        h = file_hash(f["path"])
        by_hash.setdefault(h, []).append(f)
    return {h: paths for h, paths in by_hash.items() if len(paths) > 1}
```

## Batch Rename

```python
import re

def batch_rename(directory, pattern, replacement, dry_run=True):
    results = []
    for f in os.listdir(directory):
        old_path = os.path.join(directory, f)
        if os.path.isfile(old_path):
            new_name = re.sub(pattern, replacement, f)
            if new_name != f:
                new_path = os.path.join(directory, new_name)
                results.append({"from": f, "to": new_name})
                if not dry_run:
                    os.rename(old_path, new_path)
    return results

# Examples:
# batch_rename(".", r"\s+", "_")         # Replace spaces with underscores
# batch_rename(".", r"\(\d+\)", "")       # Remove (1), (2) suffixes
# batch_rename(".", r"Screenshot_(\d+)", r"ss_\1")  # Rename Screenshot_001 → ss_001
```

## Clean Temp Files

```python
TEMP_PATTERNS = [
    r"\.tmp$", r"\.temp$", r"~$", r"^~\$",     # Temp files
    r"\.log$",                                    # Log files
    r"\.bak$", r"\.backup$",                      # Backups
    r"\.cache$", r"\.pyc$",                       # Cache
    r"^Thumbs\.db$", r"^\.DS_Store$",             # System junk
]
```

## Verification

```python
# After organizing:
before_count = 150
after_count = sum(len(files) for files in os.listdir(target) if os.path.isdir(f))

print(f"Files organized: {before_count}")
print(f"Destination folders: {len([d for d in os.listdir(target) if os.path.isdir(os.path.join(target, d))])}")
print(f"Undo available: {os.path.exists(os.path.join(target, '.organizer_undo.json'))}")
```
