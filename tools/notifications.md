Show Gitea notifications via the tea CLI; returns JSON array of unread/pinned items by default.

<usage>
- mine=true shows notifications across all your repositories (default: current repo)
- types: comma-separated filter — issue, pull, repository, commit
- states: comma-separated — pinned, unread, read (default: unread,pinned)
- limit controls max results (default 20)
</usage>

<requirements>
- tea CLI must be installed and logged in (tea login)
- Run `tea whoami` to confirm active session
</requirements>

<tips>
- Use mine=true for a global inbox view across all repos
- Filter types=pull to focus only on PR activity
</tips>
