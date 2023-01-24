package operator

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/dynamic"
	kubeclient "k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	opv1 "github.com/openshift/api/operator/v1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/ibm-powervs-block-csi-driver-operator/assets"
	"github.com/openshift/library-go/pkg/config/client"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/csi/csicontrollerset"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivercontrollerservicecontroller"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	dc "github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"github.com/openshift/library-go/pkg/operator/events"
	goc "github.com/openshift/library-go/pkg/operator/genericoperatorclient"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/staticresourcecontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

const (
	// Operand and operator run in the same namespace
	defaultNamespace   = "openshift-cluster-csi-drivers"
	operatorName       = "ibm-powervs-block-csi-driver-operator"
	operandName        = "ibm-powervs-block-csi-driver"
	secretName         = "ibm-powervs-cloud-credentials"
	trustedCAConfigMap = "ibm-powervs-block-csi-driver-trusted-ca-bundle"

	hypershiftImageEnvName  = "HYPERSHIFT_IMAGE"
	hypershiftPriorityClass = "hypershift-control-plane"
)

func RunOperator(ctx context.Context, controllerConfig *controllercmd.ControllerContext, guestKubeConfigString string) error {
	// Create core clientset and informers for MGMT cluster
	eventRecorder := controllerConfig.EventRecorder
	controlPlaneNamespace := controllerConfig.OperatorNamespace
	controlPlaneKubeClient := kubeclient.NewForConfigOrDie(rest.AddUserAgent(controllerConfig.KubeConfig, operatorName))
	controlPlaneKubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(controlPlaneKubeClient, controlPlaneNamespace, "")
	controlPlaneSecretInformer := controlPlaneKubeInformersForNamespaces.InformersFor(controlPlaneNamespace).Core().V1().Secrets()
	controlPlaneConfigMapInformer := controlPlaneKubeInformersForNamespaces.InformersFor(controlPlaneNamespace).Core().V1().ConfigMaps()

	controlPlaneDynamicClient, err := dynamic.NewForConfig(controllerConfig.KubeConfig)
	if err != nil {
		return err
	}

	// Create  clientset and informers for GUEST cluster
	guestNamespace := defaultNamespace
	guestKubeConfig := controllerConfig.KubeConfig
	guestKubeClient := controlPlaneKubeClient
	isHypershift := guestKubeConfigString != ""
	if isHypershift {
		guestKubeConfig, err = client.GetKubeConfigOrInClusterConfig(guestKubeConfigString, nil)
		if err != nil {
			return err
		}
		guestKubeClient = kubeclient.NewForConfigOrDie(rest.AddUserAgent(guestKubeConfig, operatorName))

		// Create all events in the GUEST cluster.
		// Use name of the operator Deployment in the management cluster + namespace
		// in the guest cluster as the closest approximation of the real involvedObject.
		controllerRef, err := events.GetControllerReferenceForCurrentPod(ctx, controlPlaneKubeClient, controlPlaneNamespace, nil)
		controllerRef.Namespace = guestNamespace
		if err != nil {
			klog.Warningf("unable to get owner reference (falling back to namespace): %v", err)
		}
		eventRecorder = events.NewKubeRecorder(guestKubeClient.CoreV1().Events(guestNamespace), operandName, controllerRef)
	}

	guestDynamicClient, err := dynamic.NewForConfig(guestKubeConfig)
	if err != nil {
		return err
	}

	// Client informers for the GUEST cluster.
	guestKubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(guestKubeClient, guestNamespace, "")
	guestConfigMapInformer := guestKubeInformersForNamespaces.InformersFor(guestNamespace).Core().V1().ConfigMaps()
	guestNodeInformer := guestKubeInformersForNamespaces.InformersFor("").Core().V1().Nodes()

	guestConfigClient := configclient.NewForConfigOrDie(rest.AddUserAgent(guestKubeConfig, operatorName))
	guestConfigInformers := configinformers.NewSharedInformerFactory(guestConfigClient, 20*time.Minute)
	guestInfraInformer := guestConfigInformers.Config().V1().Infrastructures()

	// Create client and informers for ClusterCSIDriver CR.
	gvr := opv1.SchemeGroupVersion.WithResource("clustercsidrivers")
	guestOperatorClient, guestDynamicInformers, err := goc.NewClusterScopedOperatorClientWithConfigName(guestKubeConfig, gvr, string(opv1.IBMPowerVSBlockCSIDriver))
	if err != nil {
		return err
	}

	controlPlaneInformersForEvents := []factory.Informer{
		controlPlaneSecretInformer.Informer(),
		controlPlaneConfigMapInformer.Informer(),
		guestNodeInformer.Informer(),
		guestInfraInformer.Informer(),
	}

	controlPlaneCSIControllerSet := csicontrollerset.NewCSIControllerSet(
		guestOperatorClient,
		eventRecorder,
	).WithLogLevelController().WithManagementStateController(
		operandName,
		false,
	).WithStaticResourcesController(
		"PowerVSBlockCSIDriverControlPlaneStaticResourcesController",
		controlPlaneKubeClient,
		controlPlaneDynamicClient,
		controlPlaneKubeInformersForNamespaces,
		assetWithNamespaceFunc(controlPlaneNamespace),
		[]string{
			"controller_sa.yaml",
			"controller_pdb.yaml",
			"cabundle_cm.yaml",
		},
	).WithCSIConfigObserverController(
		"PowerVSBlockDriverCSIConfigObserverController",
		guestConfigInformers,
	).WithCSIDriverControllerService(
		"PowerVSBlockDriverControllerServiceController",
		assets.ReadFile,
		"controller.yaml",
		controlPlaneKubeClient,
		controlPlaneKubeInformersForNamespaces.InformersFor(controlPlaneNamespace),
		guestConfigInformers,
		controlPlaneInformersForEvents,
		withHypershiftDeploymentHook(isHypershift, os.Getenv(hypershiftImageEnvName)),
		withHypershiftReplicasHook(isHypershift, guestNodeInformer.Lister()),
		withNamespaceDeploymentHook(controlPlaneNamespace),
		csidrivercontrollerservicecontroller.WithSecretHashAnnotationHook(controlPlaneNamespace, secretName, controlPlaneSecretInformer),
		csidrivercontrollerservicecontroller.WithObservedProxyDeploymentHook(),
		csidrivercontrollerservicecontroller.WithCABundleDeploymentHook(
			controlPlaneNamespace,
			trustedCAConfigMap,
			controlPlaneConfigMapInformer,
		),
		csidrivercontrollerservicecontroller.WithSecretHashAnnotationHook(
			controlPlaneNamespace,
			secretName,
			controlPlaneSecretInformer,
		),
	)

	if err != nil {
		return err
	}

	// Start controllers that manage resources in GUEST clusters.
	guestCSIControllerSet := csicontrollerset.NewCSIControllerSet(
		guestOperatorClient,
		eventRecorder,
	).WithStaticResourcesController(
		"PowerVSBlockDriverGuestStaticResourcesController",
		guestKubeClient,
		guestDynamicClient,
		guestKubeInformersForNamespaces,
		assets.ReadFile,
		[]string{
			"storageclass_tier1.yaml",
			"storageclass_tier3.yaml",
			"csidriver.yaml",
			"node_sa.yaml",
			"rbac/privileged_role.yaml",
			"rbac/node_privileged_binding.yaml",
			"rbac/csi_node_role.yaml",
			"rbac/csi_node_binding.yaml",
		},
	).WithCSIDriverNodeService(
		"PowerVSBlockDriverNodeServiceController",
		assets.ReadFile,
		"node.yaml",
		guestKubeClient,
		guestKubeInformersForNamespaces.InformersFor(guestNamespace),
		[]factory.Informer{guestConfigMapInformer.Informer()},
		csidrivernodeservicecontroller.WithObservedProxyDaemonSetHook(),
		csidrivernodeservicecontroller.WithCABundleDaemonSetHook(
			guestNamespace,
			trustedCAConfigMap,
			guestConfigMapInformer,
		),
	)
	if !isHypershift {
		staticResourcesController := staticresourcecontroller.NewStaticResourceController(
			"PowerVSBlockStaticResourcesController",
			assets.ReadFile,
			[]string{
				"rbac/attacher_role.yaml",
				"rbac/attacher_binding.yaml",
				"rbac/provisioner_role.yaml",
				"rbac/provisioner_binding.yaml",
				"rbac/resizer_role.yaml",
				"rbac/resizer_binding.yaml",
				"service.yaml",
				"rbac/prometheus_role.yaml",
				"rbac/prometheus_rolebinding.yaml",
				"rbac/kube_rbac_proxy_role.yaml",
				"rbac/kube_rbac_proxy_binding.yaml",
			},
			(&resourceapply.ClientHolder{}).WithKubernetes(controlPlaneKubeClient).WithDynamicClient(controlPlaneDynamicClient),
			guestOperatorClient,
			eventRecorder,
		).AddKubeInformers(controlPlaneKubeInformersForNamespaces)

		klog.Info("Starting static resources controller")
		go staticResourcesController.Run(ctx, 1)

		serviceMonitorController := staticresourcecontroller.NewStaticResourceController(
			"PowerVSBlockCSIServiceMonitorController",
			assets.ReadFile,
			[]string{"servicemonitor.yaml"},
			(&resourceapply.ClientHolder{}).WithDynamicClient(controlPlaneDynamicClient),
			guestOperatorClient,
			eventRecorder,
		).WithIgnoreNotFoundOnCreate()

		klog.Info("Starting ServiceMonitor controller")
		go serviceMonitorController.Run(ctx, 1)
	}

	klog.Info("Starting control plane informers")
	go controlPlaneKubeInformersForNamespaces.Start(ctx.Done())

	klog.Info("Starting control plane controllerset")
	go controlPlaneCSIControllerSet.Run(ctx, 1)

	klog.Info("Starting guest cluster informers")
	go guestKubeInformersForNamespaces.Start(ctx.Done())
	go guestDynamicInformers.Start(ctx.Done())
	go guestConfigInformers.Start(ctx.Done())

	klog.Info("Starting guest cluster controllerset")
	go guestCSIControllerSet.Run(ctx, 1)

	<-ctx.Done()

	return fmt.Errorf("stopped")
}

func assetWithNamespaceFunc(namespace string) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		content, err := assets.ReadFile(name)
		if err != nil {
			panic(err)
		}
		return bytes.ReplaceAll(content, []byte("${NAMESPACE}"), []byte(namespace)), nil
	}
}

func withNamespaceDeploymentHook(namespace string) dc.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		deployment.Namespace = namespace
		return nil
	}
}

func withHypershiftReplicasHook(isHypershift bool, guestNodeLister corev1listers.NodeLister) dc.DeploymentHookFunc {
	if !isHypershift {
		return csidrivercontrollerservicecontroller.WithReplicasHook(guestNodeLister)
	}
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		// TODO: get this information from HostedControlPlane.Spec.AvailabilityPolicy
		replicas := int32(1)
		deployment.Spec.Replicas = &replicas
		return nil
	}
}

func withHypershiftDeploymentHook(isHypershift bool, hypershiftImage string) dc.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		if !isHypershift {
			return nil
		}

		deployment.Spec.Template.Spec.PriorityClassName = hypershiftPriorityClass

		// Inject into the pod the volumes used by CSI and token minter sidecars.
		podSpec := &deployment.Spec.Template.Spec
		podSpec.Volumes = append(podSpec.Volumes,
			corev1.Volume{
				Name: "hosted-kubeconfig",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						// FIXME: use a ServiceAccount from the guest cluster
						SecretName: "service-network-admin-kubeconfig",
					},
				},
			},
		)

		// The bound-sa-token volume must be an empty disk in Hypershift.
		for i := range podSpec.Volumes {
			if podSpec.Volumes[i].Name != "bound-sa-token" {
				continue
			}
			podSpec.Volumes[i].VolumeSource = corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium: corev1.StorageMediumMemory,
				},
			}
		}

		// The metrics-serving-cert volume is not used in Hypershift.
		for i := range podSpec.Volumes {
			if podSpec.Volumes[i].Name == "metrics-serving-cert" {
				podSpec.Volumes = append(podSpec.Volumes[:i], podSpec.Volumes[i+1:]...)
				break
			}
		}

		filtered := []corev1.Container{}
		for i := range podSpec.Containers {
			switch podSpec.Containers[i].Name {
			case "driver-kube-rbac-proxy":
			case "provisioner-kube-rbac-proxy":
			case "attacher-kube-rbac-proxy":
			case "resizer-kube-rbac-proxy":
			case "snapshotter-kube-rbac-proxy":
			default:
				filtered = append(filtered, podSpec.Containers[i])
			}
		}
		podSpec.Containers = filtered

		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: "powervs-storage-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "storage-config",
					},
				},
			},
		})

		// Inject into the CSI sidecars the hosted Kubeconfig.
		for i := range podSpec.Containers {
			container := &podSpec.Containers[i]
			switch container.Name {
			case "csi-driver":
				container.Args = append(container.Args, "--cloud-config=/etc/cloud-conf/storage-config")

				container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
					Name:      "powervs-storage-config",
					MountPath: "/etc/cloud-conf",
				})
			case "node-update-controller":
			case "csi-provisioner":
			case "csi-attacher":
			case "csi-snapshotter":
			case "csi-resizer":
			default:
				continue
			}
			container.Args = append(container.Args, "--kubeconfig=$(KUBECONFIG)")
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  "KUBECONFIG",
				Value: "/etc/hosted-kubernetes/kubeconfig",
			})
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      "hosted-kubeconfig",
				MountPath: "/etc/hosted-kubernetes",
				ReadOnly:  true,
			})
		}

		// Add the token minter sidecar into the pod.
		podSpec.Containers = append(podSpec.Containers, corev1.Container{
			Name:            "token-minter",
			Image:           hypershiftImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/usr/bin/control-plane-operator", "token-minter"},
			Args: []string{
				"--service-account-namespace=openshift-cluster-csi-drivers",
				"--service-account-name=ibm-powervs-block-csi-driver-controller-sa",
				"--token-audience=openshift",
				"--token-file=/var/run/secrets/openshift/serviceaccount/token",
				"--kubeconfig=/etc/hosted-kubernetes/kubeconfig",
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10m"),
					corev1.ResourceMemory: resource.MustParse("10Mi"),
				},
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "bound-sa-token",
					MountPath: "/var/run/secrets/openshift/serviceaccount",
				},
				{
					Name:      "hosted-kubeconfig",
					MountPath: "/etc/hosted-kubernetes",
					ReadOnly:  true,
				},
			},
		})

		return nil
	}
}
