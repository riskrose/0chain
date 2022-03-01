#!/bin/bash
set -e

# Install msgp
go install github.com/tinylib/msgp@v1.1.6

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

echo "0chain dir: ${DIR}"

pushd "${DIR}/../../code/go/0chain.net" >/dev/null

go generate -run=msgp ./... >/dev/null

changes=$(git diff | wc -l | tr -d ' ')

if [[ $changes != 0 ]]; then echo 'Changes detected on running go generate: msgp'; exit 2; fi


popd >/dev/null
