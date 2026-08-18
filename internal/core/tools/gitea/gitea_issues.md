List issues from a Gitea repository via the tea CLI; returns JSON array sorted by update time.

<usage>
- Omit repo to use the repository detected from $PWD
- state: open (default), closed, or all
- Use keyword to full-text search issue titles/bodies
- Use labels for comma-separated label filtering
- limit controls max results (default 20)
</usage>

<requirements>
- tea CLI must be installed and logged in (tea login)
- Run `tea whoami` to confirm active session
</requirements>

<tips>
- Combine keyword + labels for precise filtering
- Use state=all to see both open and closed issues
- For large repos, reduce limit and paginate manually
</tips>
