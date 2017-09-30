#!/usr/bin/env bash
# File managed by pluginsync

# http://www.apache.org/licenses/LICENSE-2.0.txt
#
#
# Copyright 2016 Intel Corporation
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

set -e
set -u
set -o pipefail

__dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
__proj_dir="$(dirname "$__dir")"

# shellcheck source=scripts/common.sh
. "${__dir}/common.sh"

plugin_name=${__proj_dir##*/}
build_dir="${__proj_dir}/build"
pkg_dir="${__proj_dir}/pkg"
go_build=(go build -ldflags "-w")

_info "project path: ${__proj_dir}"
_info "plugin name: ${plugin_name}"

export CGO_ENABLED=0

# rebuild binaries:
_debug "removing: ${build_dir:?}/*"
rm -rf "${build_dir:?}/"*

_debug "removing: ${pkg_dir:?}/*"
rm -rf "${pkg_dir:?}/"*

_info "building plugin: ${plugin_name}"
export GOOS=linux
export GOARCH=amd64
mkdir -p "${build_dir}/${GOOS}/x86_64"
"${go_build[@]}" -o "${build_dir}/${GOOS}/x86_64/${plugin_name}" . || exit 1

# build package for the Intel agent
mkdir -p pkg/tmp/opt/snap/plugins
cp -f "${build_dir}/${GOOS}/x86_64/${plugin_name}" pkg/tmp/opt/snap/plugins
(cd ${pkg_dir} && \
fpm -s dir -C tmp -t deb \
  -n ${plugin_name} \
  -m "Papertrails Ops <ops@papertrail.com>" \
  -v 5 \
  -d snap-telemetry \
  --url "https://www.papertrail.com" \
  --description "DBI plugin for the Intel snap agent" \
  --vendor "Papertrail" \
  opt/snap/plugins/snap-plugin-collector-dbi)
