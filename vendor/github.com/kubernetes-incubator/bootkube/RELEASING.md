# Preparing a bootkube release

## Versioning Notes

### Bootkube Versioning

Historically, we bump the minor version when we start supporting a new kubernetes minor version. This means:

```
v0.1.0 -> Kubernetes v1.3.x
v0.2.0 -> Kubernetes v1.4.x
v0.3.0 -> Kubernetes v1.5.x
v0.4.0 -> Kubernetes v1.6.x
```

However, as the tool has stabilized in functionality, and we head toward a v1.0, we should also begin bumping minor versions on breaking changes.

A breaking change is considered an incompability between `bootkube render` assets of one version not being able to be used with a newer `bootkube start`.

In those situations we should also begin bumping the minor version to communicate that new assets might need to be generated, or that existing assets need to be updated.

### Checkpointer Versioning

The checkpointer is developed in the same repo, but can be thought of as an independent project.
Because of this we do not use tags for the checkpointer releases, as they would intermix with bootkube releases (which do not coincide).

Instead, checkpointers are released using the last git-hash of the changes added to the checkpointer subtree.
Available releases can be seen on the Quay repository: https://quay.io/repository/coreos/pod-checkpointer.
If there were no changes made to the checkpointer subtree, a new release is not necessary.

Eventually we might want to consider moving the checkpointer to its own repo. This would allow for independent development / release cycle, which would also benefit other projects that might want to use the pod-checkpointer.
However, this should also be balanced against the longer-term goal, which would be that checkpointing is natively supported in the kubelet.

For some past discussions related to these topics, see:
- https://github.com/kubernetes/kubeadm/issues/131
- https://github.com/kubernetes/kubernetes/issues/489
- https://github.com/kubernetes-incubator/bootkube/issues/424

## Updating Kubernetes Version

### Updating Kubernetes vendor code

Vendoring currently relies on the [dep](https://github.com/golang/dep) tool. 

- Update pinned versions in `Gopkg.toml`
- Run `make vendor`

Note that we require all dependencies be recorded `Gopkg.toml`, even transitive dependencies.

### Updating hyperkube image / Kubernetes version

- Update hyperkube image for manifests in templates:
    - `pkg/asset/internal/templates.go`
- Update conformance test version: (`CONFORMANCE_VERSION`)
    -  `hack/tests/conformance-test.sh`
- Update on-host kubelet versions (`KUBELET_IMAGE_TAG`)
    - `hack/multi-node/user-data.sample`
    - `hack/single-node/user-data.sample`
    - `hack/quickstart/kubelet.master`
    - `hack/quickstart/kubelet.worker`

## Run conformance test

Easiest is to use internal jenkins job: [bootkube-development](https://jenkins-kube-lifecycle.prod.coreos.systems/view/bootkube/job/bootkube-dev/)

Or, manually:

```
# GCE
./hack/tests/conformance-gce.sh
```

```
# Vagrant
make conformance-multi
```

```
# Other
./hack/tests/conformance-test.sh
```

### Tag a release

```
git tag -s vX.Y.Z
git push origin vX.Y.Z
```

### Cut a release image

Easiest is to use internal jenkins job: [bootkube-release](https://jenkins-kube-lifecycle.prod.coreos.systems/view/bootkube/job/bootkube-release/).
This job will push the image to the quay.io/coreos/bootkube repo, and archive a tarball of binary releases (manually upload to github release)

Or, manually:

```
git checkout vX.Y.Z
make release
PUSH_IMAGE=true ./build/build-image.sh
```

# Updating checkpointer

This only needs to happen when changes have been made to the checkpointer code / container.

### Build a new checkpointer image

Easiest is to use internal jenkin job: [checkpointer-release](https://jenkins-kube-lifecycle.prod.coreos.systems/view/bootkube/job/checkpointer-release/)

Or, manually:

```
git checkout master # Checkpointer releases should only be built from commits reachable by master
make release
BUILD_IMAGE=checkpoint PUSH_IMAGE=true ./build/build-image.sh
```

### Update checkpointer manifest

In `pkg/asset/images.go` change:

`PodCheckpointer` in `DefaultImages` to use the image built in previous step.

