#!/bin/bash

set -e
MAX_VERSION="v1.33.0"
NEW_RELEASE_BRANCHES=()

# Define temporary files
rancher_tags_file=$(mktemp -p /tmp)

# Kubernetes BOT user
k8s_bot_user="k8s-release-robot@users.noreply.github.com"

# Extract the latest tag from rancher/kubernetes
git for-each-ref --sort='-creatordate' --format '%(refname:short)' refs/tags > "$rancher_tags_file"

# Check if upstream_tags_file is empty
if [ ! -s "$rancher_tags_file" ]; then
    echo "[ERROR] No tags found in rancher/kubernetes."
    rm -f "$rancher_tags_file"
    exit 1
fi

echo "[INFO] Setting up git kubernetes/kubenetes upstream in rancher/kubernetes git repository."
# Add upstream remote if not already added
if ! git remote get-url upstream &>/dev/null; then
    git remote add "upstream" https://github.com/kubernetes/kubernetes.git
fi

# Process each tag
for tag in $NEW_TAGS; do
    echo "========================================================================================"
    echo "[INFO] Processing version: ${tag}"

    # Fetch the specific tag from upstream
    git fetch upstream refs/tags/${tag}:refs/tags/${tag} --no-tags

    # Skip if tag > MAX_VERSION
    if [[ "$(printf '%s\n' "$tag" "$MAX_VERSION" | sort -V | tail -1)" == "$tag" ]]; then
        echo "[INFO] Skipping version $tag as it is >= $MAX_VERSION"
        continue
    fi
    
    # Check if the branch already exist
    if git show-ref --verify --quiet refs/remotes/origin/release-${tag}; then
        echo "[WARN] Branch release-${tag} already exist. Skipping the version ${tag}."
        continue
    fi
    
    if ! $(git checkout -qb "release-${tag}" $tag); then
        echo "[WARN] Could not checkout a local branch release-${tag} from the upstream tag ${tag}."
        continue
    fi
    echo "[INFO] Checkout to a local branch release-${tag} from the upstream tag ${tag}."


    # Extract major and minor version from the tag
    major_minor=$(echo "${tag}" | cut -d '.' -f 1,2)

    # Try to find the latest tag with the same major and minor version
    last_latest_tag=$(grep "${major_minor}" "$rancher_tags_file" | head -1)

    # If not found, look for the previous minor version
    if [ -z "$last_latest_tag" ]; then
        major_minor=$(echo "${major_minor}" | awk -F. '{print $1 "." $2-1}')
        last_latest_tag=$(grep "${major_minor}" "$rancher_tags_file" | head -1)
    fi
    echo "[INFO] Latest kubernetes version in rancher/kubernetes prior ${tag}: ${last_latest_tag}"

    # Find commits hash of latest commit from a specific user
    latest_commit_of_user=$(git log --format='%H' --author="${k8s_bot_user}" "${last_latest_tag}" | head -1)
    if [ -z "$latest_commit_of_user" ]; then
        echo "[WARN] No commit found from the user ${k8s_bot_user} in tag "${last_latest_tag}". Skipping the version ${tag}."
        continue
    fi
    echo "[INFO] Latest commit hash of ${k8s_bot_user} user: ${latest_commit_of_user}"

    head_of_last_latest_tag=$(git rev-list "${last_latest_tag}" | head -1)
    echo "[INFO] Head commit hash of tag ${last_latest_tag}: ${head_of_last_latest_tag}"

    # List of commits to cherry pick
    cherry_pick_commits=$(git rev-list --no-merges --reverse --ancestry-path "${last_latest_tag}" "$latest_commit_of_user".."$head_of_last_latest_tag")

    FAIL=0
    # Cherry-pick all commits before the user's commit
    for commit in $cherry_pick_commits; do
        if [[ $(git log --format=%B -n 1 $commit) == *"vendor update"* ]]; then
            echo "[INFO] This is a vendor commit, not cherry picking."
            echo "[INFO] Performing './hack/update-vendor.sh'"
            if ! ./hack/update-vendor.sh > /dev/null; then
                echo "[WARN] Failed during vendor update in branch release-${tag}. Skipping the version ${tag}."
                FAIL=1
                break
            fi
            echo "[INFO] Commit vendor update changes"
            git add .
            if ! $(git commit -m "vendor update" > /dev/null); then
                echo "[WARN] Failed in commiting vendor changes in branch release-${tag}. Skipping the version ${tag}."
                FAIL=1
                break
            fi
        else
            echo "[INFO] Cherry pick commit: $commit to branch: release-${tag}"
            if ! git cherry-pick "$commit" > /dev/null; then
                echo "[WARN] Failed during cherry-pick of commit $commit in branch release-${tag}. Skipping the version ${tag}."
                FAIL=1
                break
            fi
        fi
    done

    if [[ $FAIL == 0 ]]; then
        echo "[INFO] Cherry pick completed successfully. Pushing branch release-${tag} to rancher repository."
        if ! $(git push --quiet --no-progress origin release-${tag} > /dev/null); then
            echo "[WARN] Failed while pushing the branch release-${tag} to rancher repository. Skipping the version ${tag}."
            continue
        else
            NEW_RELEASE_BRANCHES+=( "release-${tag}" )
            echo "[INFO] Successfully pushed branch release-${tag}: https://github.com/rancher/kubernetes/tree/release-${tag}"
        fi
    else
        git cherry-pick --abort
    fi
done

echo "========================================================================================"

# Print the new branches
if [ ${#NEW_RELEASE_BRANCHES[@]} -eq 0 ]; then
    echo "[ERROR] No new release branches."
    exit 1
else
    echo "[INFO] New release branches:"
    for branch in "${NEW_RELEASE_BRANCHES[@]}"; do
        echo "- $branch"
    done
fi

# Convert NEW_RELEASE_BRANCHES array to JSON string
echo "NEW_RELEASE_BRANCHES=$(printf '%s\n' "${NEW_RELEASE_BRANCHES[@]}" | awk '{printf "\"%s\",", $0}' | sed 's/,$/]/' | sed 's/^/[/' )" >> $GITHUB_OUTPUT

# Clean up temporary files
rm -f "$rancher_tags_file"