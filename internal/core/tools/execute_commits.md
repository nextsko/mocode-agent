Execute a series of git commits in the specified order.
Each commit stages its files, then runs `git commit` with the given subject and body.
Returns the resulting commit hashes and a final status check.
