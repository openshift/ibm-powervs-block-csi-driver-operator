FROM golang:1.18.1 AS builder
WORKDIR /go/src/github.com/openshift/ibm-powervs-block-csi-driver-operator
COPY . .
RUN GOARCH=ppc64le make

FROM --platform=ppc64le registry.access.redhat.com/ubi8/ubi:8.4
COPY --from=builder /go/src/github.com/openshift/ibm-powervs-block-csi-driver-operator/ibm-powervs-block-csi-driver-operator /usr/bin/
ENTRYPOINT ["/usr/bin/ibm-powervs-block-csi-driver-operator"]
LABEL io.k8s.display-name="OpenShift IBM PowerVS Block CSI Driver Operator" \
	io.k8s.description="The IBM PowerVS Block CSI Driver Operator installs and maintains the IBM PowerVS Block CSI Driver on a cluster."
