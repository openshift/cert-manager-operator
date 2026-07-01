#!/bin/bash

set -euo pipefail

MAIN_BRANCH=master

# Update the remotes for good measure
echo "Fetching remotes..."
git remote update
git fetch

# Update the main branch first
git checkout "$MAIN_BRANCH"
git pull --rebase upstream "$MAIN_BRANCH"
git push origin "$MAIN_BRANCH"

# Loop through all local branches and rebase them against upstream/main.
# Push the rebased branch to origin and keep it locally.
for branch in $(git branch --format='%(refname:short)' | grep -v "^${MAIN_BRANCH}$"); do
    echo ""
    echo "=== Rebasing $branch ==="
    git checkout "$branch"

    if git pull --rebase upstream "$MAIN_BRANCH"; then
        echo "Pushing $branch to origin..."
        git push origin "$branch" --force-with-lease
    else
        echo "WARNING: Rebase failed for $branch, skipping push."
        git rebase --abort 2>/dev/null || true
    fi
done

# Return to the main branch when done
git checkout "$MAIN_BRANCH"
echo ""
echo "Done. All branches rebased and pushed."
