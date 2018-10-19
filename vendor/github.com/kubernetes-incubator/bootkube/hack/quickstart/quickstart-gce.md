## GCE Quickstart

### Choose a cluster prefix

This can be changed to identify separate clusters.

```
export CLUSTER_PREFIX=quickstart
```

### Launch Nodes

Launch nodes:

```
gcloud compute instances create ${CLUSTER_PREFIX}-core1 --image-project coreos-cloud --image-family coreos-stable --zone us-central1-a --machine-type n1-standard-1
```

Tag the first node as an apiserver node, and allow traffic to 6443 on that node.

```
gcloud compute instances add-tags ${CLUSTER_PREFIX}-core1 --tags ${CLUSTER_PREFIX}-apiserver --zone us-central1-a
gcloud compute firewall-rules create ${CLUSTER_PREFIX}-6443 --target-tags=${CLUSTER_PREFIX}-apiserver --allow tcp:6443
```

### Bootstrap Master

*Replace* `<node-ip>` with the EXTERNAL_IP from output of `gcloud compute instances list ${CLUSTER_PREFIX}-core1`.

```
REMOTE_USER=$USER IDENT=~/.ssh/google_compute_engine ./init-master.sh <node-ip>
```

After the master bootstrap is complete, you can continue to add worker nodes. Or cluster state can be inspected via kubectl:

```
kubectl --kubeconfig=cluster/auth/kubeconfig get nodes
```

### Add Workers

Run the `Launch Nodes` step for each additional node you wish to add (changing the name from ` ${CLUSTER_PREFIX}-core1`)

Get the EXTERNAL_IP from each node you wish to add:

```
gcloud compute instances list ${CLUSTER_PREFIX}-core2
gcloud compute instances list ${CLUSTER_PREFIX}-core3
```

Initialize each worker node by replacing `<node-ip>` with the EXTERNAL_IP from the commands above.

```
REMOTE_USER=$USER IDENT=~/.ssh/google_compute_engine ./init-node.sh <node-ip> cluster/auth/kubeconfig
```

**NOTE:** It can take a few minutes for each node to download all of the required assets / containers.
 They may not be immediately available, but the state can be inspected with:

```
kubectl --kubeconfig=cluster/auth/kubeconfig get nodes
```
