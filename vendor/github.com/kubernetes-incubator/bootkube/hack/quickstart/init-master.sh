#!/usr/bin/env bash
set -euo pipefail
set -x

REMOTE_HOST=$1
REMOTE_PORT=${REMOTE_PORT:-22}
REMOTE_USER=${REMOTE_USER:-core}
CLUSTER_DIR=${CLUSTER_DIR:-cluster}
IDENT=${IDENT:-${HOME}/.ssh/id_rsa}
SSH_OPTS=${SSH_OPTS:-}
BOOTKUBE_OPTS=${BOOTKUBE_OPTS:-}
CLOUD_PROVIDER=${CLOUD_PROVIDER:-}
NETWORK_PROVIDER=${NETWORK_PROVIDER:-flannel}

function usage() {
    echo "USAGE:"
    echo "$0: <remote-host>"
    exit 1
}

function retry_cmd() {
    set +e
    max_retries=$1; shift
    backoff=1
    retry=0
    false
    while [ $? -ne 0 ]; do
        if [ "$retry" -ge $max_retries ]; then
            break
        else
            retry=$((retry+1))
        fi
        "$@" || (sleep $((backoff *= 2)); false)
    done
    set -e
}

function wait_for_ssh() {
    retry_cmd 100 ssh -i ${IDENT} -p ${REMOTE_PORT} ${SSH_OPTS} ${REMOTE_USER}@${REMOTE_HOST} "uname -a"
}

function configure_etcd() {
    [ -f "/etc/systemd/system/etcd-member.service.d/10-etcd-member.conf" ] || {
        mkdir -p /etc/etcd/tls
        cp /home/${REMOTE_USER}/assets/tls/etcd-* /etc/etcd/tls
        mkdir -p /etc/etcd/tls/etcd
        cp /home/${REMOTE_USER}/assets/tls/etcd/* /etc/etcd/tls/etcd
        chown -R etcd:etcd /etc/etcd
        chmod -R u=rX,g=,o= /etc/etcd
        mkdir -p /etc/systemd/system/etcd-member.service.d
        cat << EOF > /etc/systemd/system/etcd-member.service.d/10-etcd-member.conf
[Service]
Environment="ETCD_IMAGE_TAG=v3.1.8"
Environment="ETCD_NAME=controller"
Environment="ETCD_INITIAL_CLUSTER=controller=https://${COREOS_PRIVATE_IPV4}:2380"
Environment="ETCD_INITIAL_ADVERTISE_PEER_URLS=https://${COREOS_PRIVATE_IPV4}:2380"
Environment="ETCD_ADVERTISE_CLIENT_URLS=https://${COREOS_PRIVATE_IPV4}:2379"
Environment="ETCD_LISTEN_CLIENT_URLS=https://0.0.0.0:2379"
Environment="ETCD_LISTEN_PEER_URLS=https://0.0.0.0:2380"
Environment="ETCD_SSL_DIR=/etc/etcd/tls"
Environment="ETCD_TRUSTED_CA_FILE=/etc/ssl/certs/etcd/server-ca.crt"
Environment="ETCD_CERT_FILE=/etc/ssl/certs/etcd/server.crt"
Environment="ETCD_KEY_FILE=/etc/ssl/certs/etcd/server.key"
Environment="ETCD_CLIENT_CERT_AUTH=true"
Environment="ETCD_PEER_TRUSTED_CA_FILE=/etc/ssl/certs/etcd/peer-ca.crt"
Environment="ETCD_PEER_CERT_FILE=/etc/ssl/certs/etcd/peer.crt"
Environment="ETCD_PEER_KEY_FILE=/etc/ssl/certs/etcd/peer.key"
EOF
    }
}

# Initialize a Master node
function init_master_node() {
    systemctl daemon-reload
    systemctl stop locksmithd; systemctl mask locksmithd

    etcd_render_flags="--etcd-servers=https://${COREOS_PRIVATE_IPV4}:2379"

    if [ "$NETWORK_PROVIDER" = "canal" ]; then
        network_provider_flags="--network-provider=experimental-canal"
    elif [ "$NETWORK_PROVIDER" = "calico" ]; then
        network_provider_flags="--network-provider=experimental-calico"
    else
        network_provider_flags="--network-provider=flannel"
    fi

    # Render cluster assets
    /home/${REMOTE_USER}/bootkube render --asset-dir=/home/${REMOTE_USER}/assets ${etcd_render_flags} ${network_provider_flags} \
      --api-servers=https://${COREOS_PUBLIC_IPV4}:6443,https://${COREOS_PRIVATE_IPV4}:6443

    # Move the local kubeconfig into expected location
    chown -R ${REMOTE_USER}:${REMOTE_USER} /home/${REMOTE_USER}/assets
    mkdir -p /etc/kubernetes
    cp /home/${REMOTE_USER}/assets/auth/kubeconfig-kubelet /etc/kubernetes/kubeconfig
    cp /home/${REMOTE_USER}/assets/auth/kubeconfig /etc/kubernetes/kubeconfig-admin
    cp /home/${REMOTE_USER}/assets/tls/ca.crt /etc/kubernetes/ca.crt

    # Start etcd.
    configure_etcd
    systemctl enable etcd-member; sudo systemctl start etcd-member

    # Set cloud provider
    sed -i "s/cloud-provider=/cloud-provider=$CLOUD_PROVIDER/" /etc/systemd/system/kubelet.service

    # Configure master label and taint
    echo -e 'node_label=node-role.kubernetes.io/master\nnode_taint=node-role.kubernetes.io/master=:NoSchedule' > /etc/kubernetes/kubelet.env

    # Start the kubelet
    systemctl enable kubelet; sudo systemctl start kubelet

    # Start bootkube to launch a self-hosted cluster
    /home/${REMOTE_USER}/bootkube start ${BOOTKUBE_OPTS} --asset-dir=/home/${REMOTE_USER}/assets
}

[ "$#" == 1 ] || usage

# This script can execute on a remote host by copying itself + bootkube binary + kubelet service unit to remote host.
# After assets are available on the remote host, the script will execute itself in "local" mode.
if [ "${REMOTE_HOST}" != "local" ]; then

    # wait for ssh to be ready
    wait_for_ssh

    # Set up the kubelet.service on remote host
    scp -i ${IDENT} -P ${REMOTE_PORT} ${SSH_OPTS} kubelet.service ${REMOTE_USER}@${REMOTE_HOST}:/home/${REMOTE_USER}/kubelet.service
    ssh -i ${IDENT} -p ${REMOTE_PORT} ${SSH_OPTS} ${REMOTE_USER}@${REMOTE_HOST} "sudo mv /home/${REMOTE_USER}/kubelet.service /etc/systemd/system/kubelet.service"

    # Copy bootkube binary to remote host.
    if [ -e "../../_output/bin/linux/bootkube" ]; then
        scp -i ${IDENT} -P ${REMOTE_PORT} -C ${SSH_OPTS} ../../_output/bin/linux/bootkube ${REMOTE_USER}@${REMOTE_HOST}:/home/${REMOTE_USER}/bootkube
    else
        echo "Error: Local static bootkube binary not found."
        echo "Falling back to bootkube binary in path."
        if which bootkube &>/dev/null; then
            scp -i ${IDENT} -P ${REMOTE_PORT} -C ${SSH_OPTS} "$(which bootkube)" ${REMOTE_USER}@${REMOTE_HOST}:/home/${REMOTE_USER}/bootkube
        else
            echo "Error: bootkube not found in PATH."
            exit 1
        fi
    fi
    # Copy self to remote host so script can be executed in "local" mode
    scp -i ${IDENT} -P ${REMOTE_PORT} ${SSH_OPTS} ${BASH_SOURCE[0]} ${REMOTE_USER}@${REMOTE_HOST}:/home/${REMOTE_USER}/init-master.sh
    ssh -i ${IDENT} -p ${REMOTE_PORT} ${SSH_OPTS} ${REMOTE_USER}@${REMOTE_HOST} "sudo BOOTKUBE_OPTS=${BOOTKUBE_OPTS} REMOTE_USER=${REMOTE_USER} CLOUD_PROVIDER=${CLOUD_PROVIDER} NETWORK_PROVIDER=${NETWORK_PROVIDER} /home/${REMOTE_USER}/init-master.sh local"

    # Copy assets from remote host to a local directory. These can be used to launch additional nodes & contain TLS assets
    mkdir -p ${CLUSTER_DIR}
    scp -q -i ${IDENT} -P ${REMOTE_PORT} ${SSH_OPTS} -r ${REMOTE_USER}@${REMOTE_HOST}:/home/${REMOTE_USER}/assets/* ${CLUSTER_DIR}

    # Cleanup
    ssh -i ${IDENT} -p ${REMOTE_PORT} ${SSH_OPTS} ${REMOTE_USER}@${REMOTE_HOST} "rm -rf /home/${REMOTE_USER}/{assets,init-master.sh,bootkube}"

    echo "Cluster assets copied to ${CLUSTER_DIR}"
    echo
    echo "Bootstrap complete. Access your kubernetes cluster using:"
    echo "kubectl --kubeconfig=${CLUSTER_DIR}/auth/kubeconfig get nodes"
    echo
    echo "Additional nodes can be added to the cluster using:"
    echo "./init-node.sh <node-ip> ${CLUSTER_DIR}/auth/kubeconfig-kubelet"
    echo

# Execute this script locally on the machine, assumes a kubelet.service file has already been placed on host.
elif [ "$1" == "local" ]; then
    init_master_node
fi
