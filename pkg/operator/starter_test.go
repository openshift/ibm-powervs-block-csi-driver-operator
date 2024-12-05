package operator

import (
	"testing"
	"time"

	v1 "github.com/openshift/api/config/v1"
	fakeconfig "github.com/openshift/client-go/config/clientset/versioned/fake"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func TestWithCustomEndPoint(t *testing.T) {
	tests := []struct {
		name            string
		customEndPoints []v1.PowerVSServiceEndpoint
		inDeployment    *appsv1.Deployment
		expected        *appsv1.Deployment
	}{
		{
			name:            "standard deployment with no configured service endpoints",
			customEndPoints: []v1.PowerVSServiceEndpoint{},
			inDeployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name: "csi-driver",
								Env: []corev1.EnvVar{
									{
										Name:  "IBMCLOUD_APIKEY",
										Value: "testKey",
									},
								},
							}},
						},
					},
				},
			},
			expected: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name: "csi-driver",
								Env: []corev1.EnvVar{
									{
										Name:  "IBMCLOUD_APIKEY",
										Value: "testKey",
									},
								},
							}},
						},
					},
				},
			},
		},
		{
			name: "when custom endpoints are specified",
			customEndPoints: []v1.PowerVSServiceEndpoint{
				{
					Name: "iam",
					URL:  "https://iam.ibmcloud-test.com",
				},
				{
					Name: "rc",
					URL:  "https://rc.ibmcloud-test.com",
				},
				{
					Name: "pi",
					URL:  "https://pi.ibmcloud-test.com",
				},
			},
			inDeployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name: "csi-driver",
								Env: []corev1.EnvVar{
									{
										Name:  "IBMCLOUD_APIKEY",
										Value: "testKey",
									},
								},
							}},
						},
					},
				},
			},
			expected: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name: "csi-driver",
								Env: []corev1.EnvVar{
									{
										Name:  "IBMCLOUD_APIKEY",
										Value: "testKey",
									},
									{
										Name:  "IBMCLOUD_IAM_API_ENDPOINT",
										Value: "https://iam.ibmcloud-test.com",
									},
									{
										Name:  "IBMCLOUD_RESOURCE_CONTROLLER_API_ENDPOINT",
										Value: "https://rc.ibmcloud-test.com",
									},
									{
										Name:  "IBMCLOUD_POWER_API_ENDPOINT",
										Value: "https://pi.ibmcloud-test.com",
									},
								},
							}},
						},
					},
				},
			},
		},
		{
			name: "when custom endpoints are specified along with endpoints that aren't related to the driver",
			customEndPoints: []v1.PowerVSServiceEndpoint{
				{
					Name: "iam",
					URL:  "https://iam.ibmcloud-test.com",
				},
				{
					Name: "rc",
					URL:  "https://rc.ibmcloud-test.com",
				},
				{
					Name: "pi",
					URL:  "https://pi.ibmcloud-test.com",
				},
				{
					Name: "IBMCLOUD_COS_ENDPOINT",
					URL:  "https://cos.ibmcloud-test.com",
				},
			},
			inDeployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name: "csi-driver",
								Env: []corev1.EnvVar{
									{
										Name:  "IBMCLOUD_APIKEY",
										Value: "testKey",
									},
								},
							}},
						},
					},
				},
			},
			expected: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name: "csi-driver",
								Env: []corev1.EnvVar{
									{
										Name:  "IBMCLOUD_APIKEY",
										Value: "testKey",
									},
									{
										Name:  "IBMCLOUD_IAM_API_ENDPOINT",
										Value: "https://iam.ibmcloud-test.com",
									},
									{
										Name:  "IBMCLOUD_RESOURCE_CONTROLLER_API_ENDPOINT",
										Value: "https://rc.ibmcloud-test.com",
									},
									{
										Name:  "IBMCLOUD_POWER_API_ENDPOINT",
										Value: "https://pi.ibmcloud-test.com",
									},
								},
							}},
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			infra := &v1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: v1.InfrastructureStatus{
					PlatformStatus: &v1.PlatformStatus{
						PowerVS: &v1.PowerVSPlatformStatus{
							ServiceEndpoints: test.customEndPoints,
						},
					},
				},
			}
			configClient := fakeconfig.NewSimpleClientset(infra)
			configInformerFactory := configinformers.NewSharedInformerFactory(configClient, 0)
			configInformerFactory.Config().V1().Infrastructures().Informer().GetIndexer().Add(infra)
			stopCh := make(chan struct{})
			go configInformerFactory.Start(stopCh)
			defer close(stopCh)
			wait.Poll(100*time.Millisecond, 30*time.Second, func() (bool, error) {
				return configInformerFactory.Config().V1().Infrastructures().Informer().HasSynced(), nil
			})
			deployment := test.inDeployment.DeepCopy()
			err := withCustomEndPoint(configInformerFactory.Config().V1().Infrastructures().Lister())(nil, deployment)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if e, a := test.expected, deployment; !equality.Semantic.DeepEqual(e, a) {
				t.Errorf("unexpected deployment\nwant=%#v\ngot= %#v", e, a)
			}
		})
	}

}
