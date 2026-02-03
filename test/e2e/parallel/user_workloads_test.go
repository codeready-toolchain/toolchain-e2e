package parallel

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	openshiftappsv1 "github.com/openshift/api/apps/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestIdlerAndPriorityClass(t *testing.T) {
	t.Parallel()
	await := WaitForDeployments(t)
	hostAwait := await.Host()
	memberAwait := await.Member1()

	// user & idlers for standard workloads
	idler, idlerNoise := prepareIdlerUser(t, await, "test-idler")

	// Create payloads for both users
	podsToIdle := prepareWorkloads(t, await.Member1(), idler.Name, wait.WithSandboxPriorityClass())
	podsNoise := prepareWorkloads(t, await.Member1(), idlerNoise.Name, wait.WithSandboxPriorityClass())

	// Create more noise pods in non-user namespace
	memberAwait.CreateNamespace(t, "workloads-noise")
	externalNsPodsNoise := prepareWorkloads(t, await.Member1(), "workloads-noise", wait.WithOriginalPriorityClass())

	// Set a short timeout for one of the idler to trigger pod idling
	// The idler is currently updating its status since it's already been idling the pods. So we need to keep trying to update.
	idler, err := wait.For(t, memberAwait.Awaitility, &toolchainv1alpha1.Idler{}).
		Update(idler.Name, memberAwait.Namespace, func(i *toolchainv1alpha1.Idler) {
			i.Spec.TimeoutSeconds = 5
		})
	require.NoError(t, err)

	// Check notification was created BEFORE waiting for all pods to be deleted.
	// The notification is created almost immediately when idling starts, but it's automatically
	// deleted after a short duration (5s in e2e tests). If we wait for all pods to be deleted
	// first, the notification may already be gone by the time we check for it, causing intermittent
	// test failures.
	_, err = hostAwait.WaitForNotificationWithName(t, "test-idler-dev-idled", toolchainv1alpha1.NotificationTypeIdled, wait.UntilNotificationHasConditions(wait.Sent()))
	require.NoError(t, err)

	// Wait for the pods to be deleted
	for _, p := range podsToIdle {
		err := memberAwait.WaitUntilPodsDeleted(t, p.Namespace, wait.WithPodName(p.Name))
		require.NoError(t, err)
	}

	// make sure that "noise" pods are still there, and notification is not created for stage namespace
	_, err = memberAwait.WaitForPods(t, idlerNoise.Name, len(podsNoise), wait.PodRunning(), wait.WithPodLabel("idler", "idler"), wait.WithSandboxPriorityClass())
	require.NoError(t, err)
	_, err = memberAwait.WaitForPods(t, "workloads-noise", len(externalNsPodsNoise), wait.PodRunning(), wait.WithPodLabel("idler", "idler"), wait.WithOriginalPriorityClass())
	require.NoError(t, err)
	err = hostAwait.WaitForNotificationToNotBeCreated(t, "test-idler-stage-idled")
	require.NoError(t, err)

	// Check if notification has been deleted before creating another pod
	err = hostAwait.WaitUntilNotificationWithNameDeleted(t, "test-idler-dev-idled")
	require.NoError(t, err)

	// Create another pod and make sure it's deleted.
	// In the tests above, the Idler reconcile was triggered after we changed the Idler resource (to set a short timeout).
	// Now we want to verify that the idler reconcile is triggered without modifying the Idler resource.
	// Notification shouldn't be created again.
	pod := createStandalonePod(t, await.Member1(), idler.Name, "idler-test-pod-2") // create just one standalone pod. No need to create all possible pod controllers which may own pods.
	_, err = memberAwait.WaitForPod(t, idler.Name, "idler-test-pod-2")             // pod was created
	require.NoError(t, err)
	time.Sleep(time.Duration(2*idler.Spec.TimeoutSeconds) * time.Second)
	err = memberAwait.WaitUntilPodDeleted(t, pod.Namespace, pod.Name)
	require.NoError(t, err)
	err = hostAwait.WaitForNotificationToNotBeCreated(t, "test-idler-dev-idled")
	require.NoError(t, err)

	// There should not be any pods left in the namespace
	err = memberAwait.WaitUntilPodsDeleted(t, idler.Name, wait.WithPodLabel("idler", "idler"))
	require.NoError(t, err)

	// Wait for the AAP resource to be idled.
	// The pods are idled as well, which is verified in the previous step - after some time,
	// the idler idles the second-top-owner when the pod is still running
	clnt, err := dynamic.NewForConfig(memberAwait.RestConfig)
	require.NoError(t, err)
	_, err = memberAwait.WaitForAAP(t, "test-idler-aap", idler.Name, clnt.Resource(aapRes), true)
	require.NoError(t, err)

	// Wait for the InferenceService to be deleted - the expected action to idle
	// the workload is by deleting the InferenceService that is old enough.
	// The pods are idled as well, which is verified in the previous step - after some time,
	// the idler idles the second-top-owner when the pod is still running
	err = memberAwait.WaitUntilInferenceServiceDeleted(t, "test-idler-kserve", idler.Name, clnt.Resource(inferenceServiceRes))
	require.NoError(t, err)

	// Wait for the DataVolume to be deleted
	err = memberAwait.WaitUntilDataVolumeDeleted(t, "test-idler-datavolume", idler.Name, clnt.Resource(dataVolumeRes))
	require.NoError(t, err)

	// Wait for the PVC to be deleted
	err = memberAwait.WaitUntilPVCDeleted(t, "test-idler-pvc", idler.Name)
	require.NoError(t, err)
}

func prepareIdlerUser(t *testing.T, await wait.Awaitilities, name string) (*toolchainv1alpha1.Idler, *toolchainv1alpha1.Idler) {
	memberAwait := await.Member1()

	NewSignupRequest(await).
		Username(name).
		Email(name + "@redhat.com").
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait).
		SpaceTier("base"). // let's move it to base to have two namespaces to monitor
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)

	idler, err := memberAwait.WaitForIdler(t, name+"-dev", wait.IdlerConditions(wait.Running()))
	require.NoError(t, err)

	// Noise
	idlerNoise, err := memberAwait.WaitForIdler(t, name+"-stage", wait.IdlerConditions(wait.Running()))
	require.NoError(t, err)
	return idler, idlerNoise
}

func prepareWorkloads(t *testing.T, memberAwait *wait.MemberAwaitility, namespace string, additionalPodCriteria ...wait.PodWaitCriterion) []corev1.Pod {
	createStandalonePod(t, memberAwait, namespace, "idler-test-pod-1")
	d := createDeployment(t, memberAwait, namespace)
	n := 1 + int(*d.Spec.Replicas) // total number of created pods

	rs := createReplicaSet(t, memberAwait, namespace)
	n = n + int(*rs.Spec.Replicas)

	createDaemonSet(t, memberAwait, namespace)
	nodes := &corev1.NodeList{}
	err := memberAwait.Client.List(context.TODO(), nodes, client.MatchingLabels(map[string]string{"node-role.kubernetes.io/worker": ""}))
	require.NoError(t, err)
	n = n + len(nodes.Items) // DaemonSet creates N pods where N is the number of worker nodes in the cluster

	createJob(t, memberAwait, namespace)
	n++

	dc := createDeploymentConfig(t, memberAwait, types.NamespacedName{Namespace: namespace, Name: "idler-test-dc"})
	n = n + int(dc.Spec.Replicas)

	// create another DeploymentConfig that will be paused to test that the idler will unpause it when scaling it down
	dcPaused := createDeploymentConfig(t, memberAwait, types.NamespacedName{Namespace: namespace, Name: "idler-test-dc-paused"})
	n = n + int(dcPaused.Spec.Replicas)

	rc := createReplicationController(t, memberAwait, namespace)
	n = n + int(*rc.Spec.Replicas)

	// Create an Ansible Automation Platform resource
	aapDeployment := createAAP(t, memberAwait, "test-idler-aap", namespace)
	n = n + int(*aapDeployment.Spec.Replicas)

	// Create a DataVolume resource with manual PVC and Pod
	createDataVolume(t, memberAwait, "test-idler-datavolume", namespace)
	n = n + 1

	// Create a PVC and Pod
	createPvcWithPod(t, memberAwait, "test-idler-pvc", namespace, nil)
	n = n + 1

	servingRuntimeDeployment := createKServeWorkloads(t, memberAwait, "test-idler-kserve", namespace)
	n = n + int(*servingRuntimeDeployment.Spec.Replicas)

	pods, err := memberAwait.WaitForPods(t, namespace, n, append(additionalPodCriteria, wait.PodRunning(),
		wait.WithPodLabel("idler", "idler"))...)
	require.NoError(t, err)

	// pause the dcPaused DeploymentConfig now that its pods are running
	pauseDeploymentConfig(t, memberAwait, types.NamespacedName{Namespace: dcPaused.Namespace, Name: dcPaused.Name})
	return pods
}

func createDeployment(t *testing.T, memberAwait *wait.MemberAwaitility, namespace string) *appsv1.Deployment {
	// Create a Deployment with two pods
	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-deployment", Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels("idler-deployment")},
			Replicas: &replicas,
			Template: podTemplateSpec("idler-deployment"),
		},
	}
	err := memberAwait.Create(t, deployment)
	require.NoError(t, err)

	return deployment
}

var aapRes = schema.GroupVersionResource{Group: "aap.ansible.com", Version: "v1alpha1", Resource: "ansibleautomationplatforms"}
var servingRuntimeRes = schema.GroupVersionResource{Group: "serving.kserve.io", Version: "v1alpha1", Resource: "servingruntimes"}
var inferenceServiceRes = schema.GroupVersionResource{Group: "serving.kserve.io", Version: "v1beta1", Resource: "inferenceservices"}
var dataVolumeRes = schema.GroupVersionResource{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datavolumes"}

// createAAP creates an instance of ansibleautomationplatforms.aap.ansible.com with one deployment owned by this instance
// returns the underlying deployment
func createAAP(t *testing.T, memberAwait *wait.MemberAwaitility, name, namespace string) *appsv1.Deployment {
	clnt, err := dynamic.NewForConfig(memberAwait.RestConfig)
	require.NoError(t, err)

	// Create an AAP instance
	aap := aapResource(name)
	createdAAP, err := clnt.Resource(aapRes).Namespace(namespace).Create(context.TODO(), aap, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create a Deployment with two replicas and the AAP instance as the owner
	replicas := int32(2)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels(name)},
			Replicas: &replicas,
			Template: podTemplateSpec(name),
		},
	}
	err = controllerutil.SetOwnerReference(createdAAP, deployment, scheme.Scheme)
	require.NoError(t, err)
	err = memberAwait.Create(t, deployment)
	require.NoError(t, err)

	return deployment
}

func createReplicaSet(t *testing.T, memberAwait *wait.MemberAwaitility, namespace string) *appsv1.ReplicaSet {
	// Standalone ReplicaSet
	replicas := int32(2)
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-replicaset", Namespace: namespace},
		Spec: appsv1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels("idler-rs")},
			Replicas: &replicas,
			Template: podTemplateSpec("idler-rs"),
		},
	}
	err := memberAwait.Create(t, rs)
	require.NoError(t, err)

	return rs
}

func createDaemonSet(t *testing.T, memberAwait *wait.MemberAwaitility, namespace string) *appsv1.DaemonSet {
	// Standalone ReplicaSet
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-daemonset", Namespace: namespace},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels("idler-ds")},
			Template: podTemplateSpec("idler-ds"),
		},
	}
	err := memberAwait.Create(t, ds)
	require.NoError(t, err)

	return ds
}

func createJob(t *testing.T, memberAwait *wait.MemberAwaitility, namespace string) *batchv1.Job {
	// Job
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-job", Namespace: namespace},
		Spec: batchv1.JobSpec{
			Template: podTemplateSpec(""),
		},
	}
	job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	err := memberAwait.Create(t, job)
	require.NoError(t, err)

	return job
}

func createDeploymentConfig(t *testing.T, memberAwait *wait.MemberAwaitility, namespacedName types.NamespacedName) *openshiftappsv1.DeploymentConfig {
	// Create a DeploymentConfig with two pods
	spec := podTemplateSpec("idler-dc")
	replicas := int32(2)
	dc := &openshiftappsv1.DeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{Name: namespacedName.Name, Namespace: namespacedName.Namespace},
		Spec: openshiftappsv1.DeploymentConfigSpec{
			Selector: selectorLabels("idler-dc"),
			Replicas: replicas,
			Template: &spec,
		},
	}
	err := memberAwait.Create(t, dc)
	require.NoError(t, err)

	return dc
}

func pauseDeploymentConfig(t *testing.T, memberAwait *wait.MemberAwaitility, namespacedName types.NamespacedName) *openshiftappsv1.DeploymentConfig {
	dc, err := wait.For(t, memberAwait.Awaitility, &openshiftappsv1.DeploymentConfig{}).
		Update(namespacedName.Name, namespacedName.Namespace,
			func(dc *openshiftappsv1.DeploymentConfig) {
				dc.Spec.Paused = true
			})
	require.NoError(t, err)
	return dc
}

func createReplicationController(t *testing.T, memberAwait *wait.MemberAwaitility, namespace string) *corev1.ReplicationController {
	// Standalone ReplicationController
	spec := podTemplateSpec("idler-rc")
	replicas := int32(2)
	rc := &corev1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-rc", Namespace: namespace},
		Spec: corev1.ReplicationControllerSpec{
			Selector: selectorLabels("idler-rc"),
			Replicas: &replicas,
			Template: &spec,
		},
	}
	err := memberAwait.Create(t, rc)
	require.NoError(t, err)

	return rc
}

func createStandalonePod(t *testing.T, memberAwait *wait.MemberAwaitility, namespace, name string) *corev1.Pod {
	// Create a Deployment with two pods
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    map[string]string{"idler": "idler"},
		},
		Spec: podSpec(),
	}
	pod.Spec.PriorityClassName = "system-cluster-critical"
	err := memberAwait.Create(t, pod)
	require.NoError(t, err)
	return pod
}

func podSpec() corev1.PodSpec {
	zero := int64(0)
	return corev1.PodSpec{
		TerminationGracePeriodSeconds: &zero,
		Containers: []corev1.Container{{
			Name:    "sleep",
			Image:   "quay.io/prometheus/busybox:latest",
			Command: []string{"sleep", "36000"}, // 10 hours
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"cpu":    resource.MustParse("1m"),
					"memory": resource.MustParse("8Mi"),
				},
				Limits: corev1.ResourceList{
					"cpu":    resource.MustParse("50m"),
					"memory": resource.MustParse("80Mi"),
				},
			},
		}},
	}
}

func podTemplateSpec(app string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
			"idler": "idler",
			"app":   app,
		}},
		Spec: podSpec(),
	}
}

func selectorLabels(app string) map[string]string {
	return map[string]string{"app": app}
}

func aapResource(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "aap.ansible.com/v1alpha1",
			"kind":       "AnsibleAutomationPlatform",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"idle_aap": false,
			},
		},
	}
}

// createDataVolume creates an instance of datavolumes.cdi.kubevirt.io with one PVC and Pod owned in chain
func createDataVolume(t *testing.T, memberAwait *wait.MemberAwaitility, name, namespace string) *corev1.Pod {
	cl, err := dynamic.NewForConfig(memberAwait.RestConfig)
	require.NoError(t, err)

	// Create a DataVolume instance
	dataVolume := dataVolumeResource(name)
	createdDataVolume, err := cl.Resource(dataVolumeRes).Namespace(namespace).Create(context.TODO(), dataVolume, metav1.CreateOptions{})
	require.NoError(t, err)

	// Manually create PVC (simulating CDI controller behavior) with DataVolume as owner
	return createPvcWithPod(t, memberAwait, name, namespace, createdDataVolume)
}

func createPvcWithPod(t *testing.T, memberAwait *wait.MemberAwaitility, name, namespace string, owner client.Object) *corev1.Pod {
	pvc := &corev1.PersistentVolumeClaim{}
	pvc.SetName(name + "-pvc")
	pvc.SetNamespace(namespace)
	pvc.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	pvc.Spec.Resources.Requests = corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}

	if owner != nil {
		require.NoError(t, controllerutil.SetOwnerReference(owner, pvc, scheme.Scheme))
	}
	require.NoError(t, memberAwait.CreateWithCleanup(t, pvc))

	// Create a Pod with PVC as owner and mount the PVC
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-pod",
			Namespace: namespace,
			Labels:    map[string]string{"idler": "idler", "app": name},
		},
		Spec: podSpec(),
	}
	// Add volume mount to the pod
	pod.Spec.Volumes = []corev1.Volume{
		{
			Name: "data-volume",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvc.Name,
				},
			},
		},
	}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{
			Name:      "data-volume",
			MountPath: "/data",
		},
	}
	require.NoError(t, controllerutil.SetOwnerReference(pvc, pod, scheme.Scheme))
	require.NoError(t, memberAwait.CreateWithCleanup(t, pod))

	return pod
}

func dataVolumeResource(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cdi.kubevirt.io/v1beta1",
			"kind":       "DataVolume",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{},
		},
	}
}

// createKServeWorkloads creates ServingRuntime and InferenceService instances with one deployment owned by ServingRuntime
// returns the underlying deployment
func createKServeWorkloads(t *testing.T, memberAwait *wait.MemberAwaitility, name, namespace string) *appsv1.Deployment {
	cl, err := dynamic.NewForConfig(memberAwait.RestConfig)
	require.NoError(t, err)

	// Create a ServingRuntime instance
	servingRuntime := servingRuntimeResource(name)
	createdServingRuntime, err := cl.Resource(servingRuntimeRes).Namespace(namespace).Create(context.TODO(), servingRuntime, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create an InferenceService instance
	inferenceService := inferenceServiceResource(name)
	_, err = cl.Resource(inferenceServiceRes).Namespace(namespace).Create(context.TODO(), inferenceService, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create a Deployment with two replicas and the ServingRuntime instance as the owner.
	// The InferenceService doesn't own anything directly.
	replicas := int32(2)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels(name)},
			Replicas: &replicas,
			Template: podTemplateSpec(name),
		},
	}
	err = controllerutil.SetOwnerReference(createdServingRuntime, deployment, scheme.Scheme)
	require.NoError(t, err)
	err = memberAwait.Create(t, deployment)
	require.NoError(t, err)

	return deployment
}

func servingRuntimeResource(name string) *unstructured.Unstructured {
	servingRuntime := &unstructured.Unstructured{}
	servingRuntime.SetAPIVersion("serving.kserve.io/v1alpha1")
	servingRuntime.SetKind("ServingRuntime")
	servingRuntime.SetName(name)
	return servingRuntime
}

func inferenceServiceResource(name string) *unstructured.Unstructured {
	inferenceService := &unstructured.Unstructured{}
	inferenceService.SetAPIVersion("serving.kserve.io/v1beta1")
	inferenceService.SetKind("InferenceService")
	inferenceService.SetName(name)
	return inferenceService
}
