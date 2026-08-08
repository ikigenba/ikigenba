package repos

const preReceiveHook = `#!/bin/sh
# repos: pre-receive. REPOS_PUSH_SCOPE is exported by the door (D19):
#   owner -> main may advance only by fast-forward; other refs unrestricted
#   run   -> main may not be touched at all; other refs unrestricted
zero=0000000000000000000000000000000000000000
while read -r old new ref; do
    [ "$ref" = "refs/heads/main" ] || continue
    if [ "$REPOS_PUSH_SCOPE" = "run" ]; then
        echo "repos: this token may not push to main" >&2
        exit 1
    fi
    if [ "$new" = "$zero" ]; then
        echo "repos: main may not be deleted" >&2
        exit 1
    fi
    [ "$old" = "$zero" ] && continue
    if ! git merge-base --is-ancestor "$old" "$new"; then
        echo "repos: force-push to main is rejected" >&2
        exit 1
    fi
done
exit 0
`
