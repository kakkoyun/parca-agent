#!/bin/bash -e

# Copyright 2022 The Parca Authors
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# tracee
# Copyright 2019 Aqua Security Software Ltd.
# Original: https://github.com/aquasecurity/tracee/blob/add1efa7934dcf46be67ea2be54ac0d139a94804/3rdparty/btfhub.sh

# This script downloads & updates 2 repos inside tracee dir structure:
#
#     1) ./3rdparty/btfhub-archive
#     2) ./3rdparty/btfhub
#
# It uses the 2 repositories to generate tailored BTF files, according to
# latest CO-RE object file, so those files can be embedded in tracee Go binary.
#
# Note: You may opt out from fetching repositories changes in the beginning of
# the execution by exporting SKIP_FETCH=1 env variable.

BASEDIR=$(dirname "${0}")
cd ${BASEDIR}/../
BASEDIR=$(pwd)
cd ${BASEDIR}

# variables

# TODO(kakkoyun): Support multiple CO-RE objects.
PARCA_AGENT_BPF_CORE="${BASEDIR}/dist/profiler/cpu.bpf.o"
BTFHUB_REPO="https://github.com/aquasecurity/btfhub.git"
BTFHUB_ARCH_REPO="https://github.com/aquasecurity/btfhub-archive.git"
BTFHUB_DIR="${BASEDIR}/3rdparty/btfhub"
BTFHUB_ARCH_DIR="${BASEDIR}/3rdparty/btfhub-archive"

# TODO(kakkoyun): Support multiple architectures. Can we cross-compile?
ARCH=$(uname -m)

case ${ARCH} in
    "x86_64")
        ARCH="x86_64"
        NOT_ARCH=("arm64")
        ;;
    "aarch64")
        ARCH="arm64"
        NOT_ARCH=("x86_64")
        ;;
    *)
        die "unsupported architecture"
        ;;
esac

die() {
    echo ${@}
    exit 1
}

branch_clean() {
    cd ${1} || die "could not change dirs"

    # small sanity check
    [ ! -f ./README.md ] && die "$(basename $(pwd)) not a repo dir"

    git fetch -a || die "could not fetch ${1}" # make sure its updated
    git clean -fdX                             # clean leftovers
    git reset --hard                           # reset letfovers
    git checkout origin/main -b main-$$
    git branch -D main
    git branch -m main-$$ main # origin/main == main

    cd ${BASEDIR}
}

# requirements

CMDS="rsync git cp rm mv"
for cmd in ${CMDS}; do
    command -v $cmd 2>&1 >/dev/null || die "cmd ${cmd} not found"
done

[ ! -f ${PARCA_AGENT_BPF_CORE} ] && die "Parca Agent CO-RE obj not found"

[ ! -d ${BTFHUB_DIR} ] && git clone --depth 1 "${BTFHUB_REPO}" ${BTFHUB_DIR}
[ ! -d ${BTFHUB_ARCH_DIR} ] && git clone --depth 1 "${BTFHUB_ARCH_REPO}" ${BTFHUB_ARCH_DIR}

# TODO(kakkoyun): Make sure we have the latest version of the repos. We will cache btfhub-archive repo.

if [ -z ${SKIP_FETCH} ]; then
    branch_clean ${BTFHUB_DIR}
    branch_clean ${BTFHUB_ARCH_DIR}
fi

cd ${BTFHUB_DIR}

#
# https://github.com/aquasecurity/btfhub/blob/main/docs/supported-distros.md
#
# remove BTFs for:
# - <= v4.18 kernels: Agent requires 4.19+
# - centos7 & v4.15 kernels: unsupported eBPF features
# - fedora 29 & 30 (from 5.3 and on) and newer: already have BTF embedded
# - amzn2: older than 4.19
#

rsync -avz \
    ${BTFHUB_ARCH_DIR}/ \
    --exclude=.git* \
    --exclude=README.md \
    --exclude="centos/7*" \
    --exclude="fedora/29/*/5.3*" \
    --exclude="fedora/30/*/5.6*" \
    --exclude="fedora/31*" \
    --exclude="fedora/32*" \
    --exclude="fedora/33*" \
    --exclude="fedora/34*" \
    --exclude="3.*" \
    --exclude="4.9*" \
    --exclude="4.14*" \
    --exclude="4.15*" \
    --exclude="amzn*" \
    ./archive/

# cleanup unneeded architectures

for not in ${NOT_ARCH[@]}; do
    rm -r $(find ./archive/ -type d -name ${not} | xargs)
done

# generate tailored BTFs

[ ! -f ./tools/btfgen.sh ] && die "could not find btfgen.sh"
./tools/btfgen.sh -a ${ARCH} -o $PARCA_AGENT_BPF_CORE

# move tailored BTFs to dist

[ ! -d ${BASEDIR}/dist ] && die "could not find dist directory"
[ ! -d ${BASEDIR}/dist/btfhub ] && mkdir ${BASEDIR}/dist/btfhub

rm -rf ${BASEDIR}/dist/btfhub/*
mv ./custom-archive/* ${BASEDIR}/dist/btfhub
