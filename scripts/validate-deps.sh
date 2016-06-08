#!/usr/bin/env bash

# Copyright 2016 The Kubernetes Authors All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
set -euo pipefail

exit_code=0

hash godir 2>/dev/null || go get -u github.com/Masterminds/godir

echo "==> Running vendor check..."
deps=$(go list -f '{{join .Deps "\n"}}' $(glide nv) | xargs go list -f '{{if not .Standard}}{{.ImportPath}}{{end}}' | grep -v "$(godir name)") || :
if [[ -n "${deps}" ]]; then
  echo "Non vendored dependencies found:"
  for d in $deps; do printf "\t%s\n" "$d"; done
  echo
  echo "These dependencies should be tracked in 'glide.yaml'."
  echo "Consider running "glide update" or "glide get" to vendor a new dependency."
  exit_code=1
fi

exit ${exit_code}
