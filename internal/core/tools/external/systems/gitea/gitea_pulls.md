List pull requests from a Gitea repository via the tea CLI; returns JSON array.

<usage>
- Omit repo to use the repository detected from $PWD
- state: open (default), closed, or all
- limit controls max results (default 20)
</usage>

<requirements>
- tea CLI must be installed and logged in (tea login)
- Run `tea whoami` to confirm active session
</requirements>

<tips>
- Use state=all to see merged and closed PRs alongside open ones
- Combine with gitea_issues using state=all to get full project activity
</tips>
