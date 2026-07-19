Scan working tree, classify changes, group into semantic commit plan.
Returns a structured CommitPlan with auto-inferred type/scope per group.
Fill in the `subject` and `body` fields, then call git_execute_commits.
