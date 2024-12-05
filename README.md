# IBM PowerVS Block CSI Driver Operator

An operator to deploy the [IBM PowerVS Block CSI Driver](https://github.com/openshift/ibm-powervs-block-csi-driver) in OKD.

This operator is installed by the [cluster-storage-operator](https://github.com/openshift/cluster-storage-operator).

# Quick start

Before running the operator manually, you must remove the operator installed by CSO/CVO

```shell
# Scale down CVO and CSO
oc scale --replicas=0 deploy/cluster-version-operator -n openshift-cluster-version
oc scale --replicas=0 deploy/cluster-storage-operator -n openshift-cluster-storage-operator

# Delete operator resources (daemonset, deployments)
oc -n openshift-cluster-csi-drivers delete deployment.apps/powervs-block-csi-driver-operator deployment.apps/ibm-powervs-block-csi-driver-controller daemonset.apps/ibm-powervs-block-csi-driver-node
```

# Define custom endpoints for the CSI driver:
Include a [serviceEndpoints](https://docs.openshift.com/container-platform/4.17/rest_api/config_apis/infrastructure-config-openshift-io-v1.html#spec-platformspec-powervs-serviceendpoints-2) section to configure non-default endpoints, which is defined
under [v1_Infrastructure](https://github.com/openshift/api/blob/master/config/v1/types_infrastructure.go#L1717-L1724)

For example:
```shell
   serviceEndpoints:
    - name: rc
      url: https://resource-controller.test.cloud.ibm.com
```
Reference:
```shell
iam - IBMCLOUD_IAM_API_ENDPOINT
rc - IBMCLOUD_RESOURCE_CONTROLLER_API_ENDPOINT
pi - IBMCLOUD_POWER_API_ENDPOINT
```
The above-mentioned set of `serviceEndpoints` are the only allowed properties. The rest are ignored.

To build and run the operator locally:

```shell
# Create only the resources the operator needs to run via CLI
oc apply -f https://raw.githubusercontent.com/openshift/cluster-storage-operator/master/assets/csidriveroperators/powervs-block/07_cr.yaml

# Build the operator
make

# Set the environment variables
export DRIVER_IMAGE=gcr.io/k8s-staging-cloud-provider-ibm/ibm-powervs-block-csi-driver:v0.1.0-alpha.3
export PROVISIONER_IMAGE=k8s.gcr.io/sig-storage/csi-provisioner:v3.1.0
export ATTACHER_IMAGE=k8s.gcr.io/sig-storage/csi-attacher:v3.4.0
export RESIZER_IMAGE=k8s.gcr.io/sig-storage/csi-resizer:v1.4.0
export NODE_DRIVER_REGISTRAR_IMAGE=k8s.gcr.io/sig-storage/csi-node-driver-registrar:v2.5.0
export LIVENESS_PROBE_IMAGE=k8s.gcr.io/sig-storage/livenessprobe:v2.6.0
export KUBE_RBAC_PROXY_IMAGE=quay.io/brancz/kube-rbac-proxy:v0.12.0

# Run the operator via CLI
./ibm-powervs-block-csi-driver-operator start --kubeconfig $KUBECONFIG --namespace openshift-cluster-csi-drivers
```