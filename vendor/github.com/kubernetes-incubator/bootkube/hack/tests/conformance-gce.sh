#!/usr/bin/env bash
set -euo pipefail

# DESCRIPTION:
#
# This script is meant to launch GCE nodes, run bootkube to bootstrap a self-hosted k8s cluster, then run conformance tests.
#
# REQUIREMENTS:
#  - gcloud cli is installed
#  - rkt is available on the host
#
# REQUIRED ENV VARS:
#  - $BUILD_ROOT: contains a checkout of bootkube at $BUILD_ROOT/bootkube
#  - $KEY_FILE:   path to GCE service account keyfile
#
# OPTIONAL ENV VARS:
#  - $COREOS_VERSION:   CoreOS image version.
#
# PROCESS:
#
# Inside a rkt container:
#   - Use gcloud to launch master node
#     - Use the quickstart init-master.sh script to run bootkube on that node
#   - Use gcloud to launch worker node(s)
#     - Use the quickstart init-node.sh script to join node to kubernetes cluster
#   - Run conformance tests against the launched cluster
#
COREOS_CHANNEL=${COREOS_CHANNEL:-'coreos-stable'}
WORKER_COUNT=4

GCE_PREFIX=${GCE_PREFIX:-'bootkube-ci'}
GCE_SERVICE_ACCOUNT=${GCE_SERVICE_ACCOUNT:-'bootkube-ci'}
GCE_PROJECT=${GCE_PROJECT:-coreos-gce-testing}

function cleanup {
    gcloud compute instances delete --quiet --zone us-central1-a ${GCE_PREFIX}-m1 || true
    gcloud compute firewall-rules delete --quiet ${GCE_PREFIX}-api-6443 || true
    for i in $(seq 1 ${WORKER_COUNT}); do
        gcloud compute instances delete --quiet --zone us-central1-a ${GCE_PREFIX}-w${i} || true
    done
    rm -rf /build/cluster
}

function init {
    curl https://storage.googleapis.com/cloud-sdk-release/google-cloud-sdk-148.0.1-linux-x86_64.tar.gz > google-cloud-sdk.tar.gz
    tar xzf google-cloud-sdk.tar.gz
    ./google-cloud-sdk/install.sh
    source ~/.bashrc
    gcloud config set project ${GCE_PROJECT}
    gcloud auth activate-service-account ${GCE_SERVICE_ACCOUNT}@${GCE_PROJECT}.iam.gserviceaccount.com --key-file=/build/keyfile
    apt-get update && apt-get install -y jq

    ssh-keygen -t rsa -f /root/.ssh/id_rsa -N ""
    awk '{print "core:" $1 " " $2 " core@conformance"}' /root/.ssh/id_rsa.pub > /root/.ssh/gce-format.pub
}

function add_master {
    gcloud compute instances create ${GCE_PREFIX}-m1 \
        --image-project coreos-cloud --image-family ${COREOS_CHANNEL} --zone us-central1-a --machine-type n1-standard-4 --boot-disk-size=30GB

    gcloud compute instances add-tags --zone us-central1-a ${GCE_PREFIX}-m1 --tags ${GCE_PREFIX}-apiserver
    gcloud compute firewall-rules create ${GCE_PREFIX}-api-6443 --target-tags=${GCE_PREFIX}-apiserver --allow tcp:6443

    gcloud compute instances add-metadata ${GCE_PREFIX}-m1 --zone us-central1-a --metadata-from-file ssh-keys=/root/.ssh/gce-format.pub

    MASTER_IP=$(gcloud compute instances list ${GCE_PREFIX}-m1 --format=json | jq --raw-output '.[].networkInterfaces[].accessConfigs[].natIP')
    cd /build/bootkube/hack/quickstart && SSH_OPTS="-o StrictHostKeyChecking=no" \
        CLUSTER_DIR=/build/cluster ./init-master.sh ${MASTER_IP}
}

function add_workers {
    #TODO (aaron): parallelize launching workers
    for i in $(seq 1 ${WORKER_COUNT}); do
        echo "Launching worker"
        gcloud compute instances create ${GCE_PREFIX}-w${i} \
            --image-project coreos-cloud --image-family ${COREOS_CHANNEL} --zone us-central1-a --machine-type n1-standard-2 --boot-disk-size=15GB

        echo "Adding ssh-key to worker metadata"
        gcloud compute instances add-metadata ${GCE_PREFIX}-w${i} --zone us-central1-a --metadata-from-file ssh-keys=/root/.ssh/gce-format.pub

        echo "Waiting 30s before retrieving worker metadata"
        sleep 30 # TODO(aaron) Have seen "Too many authentication failures" in CI jobs. This seems to help, but should dig into why
        echo "Getting worker public IP"
        local WORKER_IP=$(gcloud compute instances list ${GCE_PREFIX}-w${i} --format=json | jq --raw-output '.[].networkInterfaces[].accessConfigs[].natIP')
        cd /build/bootkube/hack/quickstart && SSH_OPTS="-o StrictHostKeyChecking=no" ./init-node.sh ${WORKER_IP} /build/cluster/auth/kubeconfig
    done
}

IN_CONTAINER=${IN_CONTAINER:-false}
if [ "${IN_CONTAINER}" == true ]; then
    #TODO(aaron): should probably run cleanup as part of init (not just on exit). Or add some random identifier to objects created during this run.
    trap cleanup EXIT
    init
    add_master
    add_workers
    KUBECONFIG=/etc/kubernetes/kubeconfig-admin WORKER_COUNT=${WORKER_COUNT} /build/bootkube/hack/tests/conformance-test.sh ${MASTER_IP} 22 /root/.ssh/id_rsa
else
    BUILD_ROOT=${BUILD_ROOT:-}
    if [ -z "$BUILD_ROOT" ]; then
        echo "BUILD_ROOT must be set"
        exit 1
    fi
    if [ -z "$KEY_FILE" ]; then
        echo "KEY_FILE must be set"
        exit 1
    fi

    RKT_OPTS=$(echo \
        "--volume buildroot,kind=host,source=${BUILD_ROOT} " \
        "--mount volume=buildroot,target=/build " \
        "--volume keyfile,kind=host,source=${KEY_FILE} " \
        "--mount volume=keyfile,target=/build/keyfile " \
    )

    #TODO(pb): See if there is a way to make the --inherit-env option replace
    #passing all the variables manually. 
    sudo rkt run --insecure-options=image ${RKT_OPTS} docker://golang:1.9.4 --exec /bin/bash -- -c \
        "IN_CONTAINER=true COREOS_CHANNEL=${COREOS_CHANNEL} GCE_PREFIX=${GCE_PREFIX} GCE_SERVICE_ACCOUNT=${GCE_SERVICE_ACCOUNT} GCE_PROJECT=${GCE_PROJECT} /build/bootkube/hack/tests/$(basename $0)"
fi
