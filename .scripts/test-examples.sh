#!/bin/bash

set -euo pipefail

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

function-hcl --insecure &
bg_pid=$!
trap 'kill ${bg_pid}; exit 1' INT
trap 'kill ${bg_pid}' EXIT

filter=${1:-}
"${SCRIPT_DIR}/gen-comp.sh" ${filter} >/dev/null
echo ---

cd  "${SCRIPT_DIR}/../example"

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
  echo $dir>&2
  cd $dir

  rm -f /tmp/functions.yaml /tmp/results.yaml
  cat functions.yaml | "${SCRIPT_DIR}/fn-dev-mode.sh" >/tmp/functions.yaml
  cmd="crossplane render -r -c xr.yaml composition.yaml /tmp/functions.yaml"
  if [[ -f observed.yaml ]]
  then
    cmd="${cmd} -o observed.yaml"
  fi
  if [[ -f extra-resources.yaml ]]
  then
    cmd="${cmd} -e extra-resources.yaml"
  fi

  ${cmd} | "${SCRIPT_DIR}/fix-output-yaml.sh" >/tmp/results.yaml
  # cat /tmp/results.yaml

  dyff between -i --omit-header --additional-identifier type --set-exit-code src/expected.yaml  /tmp/results.yaml
  cd ..
done

if [[ "${run}" == "0" ]]
then
  echo "[ERROR] No directories matched filter $filter" >&2
  exit 2
fi
