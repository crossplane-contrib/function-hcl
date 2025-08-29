#!/bin/bash

set -euo pipefail

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
cd  "${SCRIPT_DIR}/../example"

filter=${1:-}

run=0
for dir in *
do
  if [[ ! -d "${dir}" ]]
  then
    continue
  fi
  if [[ "${filter}" != "" && "${filter}" != "${dir}" ]]
  then
    continue
  fi
  run=1
  echo $dir
  (cd $dir && fn-hcl-tools package src/*hcl >/tmp/script.txtar)
  (cd $dir && cat src/comp-template.yaml | script="$(cat /tmp/script.txtar | jq -sR)" envsubst | yq -P>composition.yaml)
done

if [[ "${run}" == "0" ]]
then
  echo "[ERROR] No directories matched filter $filter" >&2
  exit 2
fi
