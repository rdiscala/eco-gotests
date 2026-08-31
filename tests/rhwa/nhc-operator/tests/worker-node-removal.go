package tests

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/bmc"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/mco"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/namespace"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/nodes"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/storage"

	. "github.com/rh-ecosystem-edge/eco-gotests/tests/rhwa/internal/rhwainittools"
	"github.com/rh-ecosystem-edge/eco-gotests/tests/rhwa/internal/rhwaparams"
	"github.com/rh-ecosystem-edge/eco-gotests/tests/rhwa/nhc-operator/internal/nhcparams"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

var _ = Describe(
	"Worker node removal",
	Ordered,
	ContinueOnFailure,
	Label(nhcparams.Label, nhcparams.LabelWorkerRemoval), func() {
		var (
			targetNode       string
			bmcClient        *bmc.BMC
			labeledWorkers   []string
			unlabeledWorkers []string
		)

		BeforeAll(func() {
			By("Checking required configuration is provided")

			if RHWAConfig.TargetWorker == "" {
				Skip("ECO_RHWA_NHC_TARGET_WORKER not set")
			}

			if RHWAConfig.StorageClass == "" {
				Skip("ECO_RHWA_NHC_STORAGE_CLASS not set")
			}

			if RHWAConfig.AppImage == "" {
				Skip("ECO_RHWA_NHC_APP_IMAGE not set")
			}

			if len(RHWAConfig.FailoverWorkers) == 0 {
				Skip("ECO_RHWA_NHC_FAILOVER_WORKERS not set")
			}

			hasHealthyFailover := false

			for _, failoverName := range RHWAConfig.FailoverWorkers {
				if failoverName == RHWAConfig.TargetWorker {
					continue
				}

				failoverNode, pullErr := nodes.Pull(APIClient, failoverName)
				if pullErr != nil {
					klog.Warningf("Failed to pull failover node %s: %v", failoverName, pullErr)

					continue
				}

				for _, condition := range failoverNode.Object.Status.Conditions {
					if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
						hasHealthyFailover = true

						break
					}
				}

				if hasHealthyFailover {
					break
				}
			}

			if !hasHealthyFailover {
				Skip("No distinct healthy failover worker available")
			}

			if RHWAConfig.TargetWorkerBMC.Address == "" {
				Skip("ECO_RHWA_NHC_TARGET_WORKER_BMC address not set")
			}

			if RHWAConfig.TargetWorkerBMC.Username == "" || RHWAConfig.TargetWorkerBMC.Password == "" {
				Skip("ECO_RHWA_NHC_TARGET_WORKER_BMC username or password not set")
			}

			targetNode = RHWAConfig.TargetWorker

			By("Waiting for target worker node to be Ready")

			klog.Infof("Waiting up to %s for node %s to become Ready", nhcparams.NodeRecoveryTimeout, targetNode)

			targetNodeObj, err := nodes.Pull(APIClient, targetNode)
			Expect(err).ToNot(HaveOccurred(),
				fmt.Sprintf("Failed to pull node %s", targetNode))

			err = targetNodeObj.WaitUntilReady(nhcparams.NodeRecoveryTimeout)
			Expect(err).ToNot(HaveOccurred(),
				fmt.Sprintf("Target worker node %s did not become Ready", targetNode))

			klog.Infof("Node %s is Ready", targetNode)

			By("Verifying NHC deployment is Ready")

			nhcDeployment, err := deployment.Pull(
				APIClient, nhcparams.OperatorDeploymentName, rhwaparams.RhwaOperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get NHC deployment")
			Expect(nhcDeployment.IsReady(rhwaparams.DefaultTimeout)).To(BeTrue(),
				"NHC deployment is not Ready")

			By("Verifying no stale SelfNodeRemediation resources exist")

			snrList, err := APIClient.Resource(rhwaparams.SnrGVR).
				Namespace(rhwaparams.RhwaOperatorNs).
				List(context.TODO(), metav1.ListOptions{})
			Expect(err).ToNot(HaveOccurred(), "Failed to list SelfNodeRemediation resources")

			if len(snrList.Items) > 0 {
				var staleNames []string
				for _, snr := range snrList.Items {
					staleNames = append(staleNames, snr.GetName())
				}

				Skip(fmt.Sprintf("Stale SelfNodeRemediation resources found: %v — "+
					"clean them up before running this test", staleNames))
			}

			By("Creating BMC client for target node")

			bmcClient = bmc.New(RHWAConfig.TargetWorkerBMC.Address).
				WithRedfishUser(RHWAConfig.TargetWorkerBMC.Username,
					RHWAConfig.TargetWorkerBMC.Password).
				WithRedfishTimeout(nhcparams.BMCTimeout)

			DeferCleanup(func() {
				By("Powering the node back on via BMC")

				if bmcClient == nil {
					return
				}

				err := bmcClient.SystemPowerOn()
				if err != nil {
					klog.Warningf("Failed to power on node: %v", err)

					return
				}

				By("Waiting for node to become Ready")

				klog.Infof("Waiting up to %s for node %s to recover to Ready state",
					nhcparams.NodeRecoveryTimeout, targetNode)

				Eventually(func() bool {
					node, pullErr := nodes.Pull(APIClient, targetNode)
					if pullErr != nil {
						klog.Infof("  waiting for node %s to reappear: %v", targetNode, pullErr)

						return false
					}

					for _, condition := range node.Object.Status.Conditions {
						if condition.Type == corev1.NodeReady {
							return condition.Status == corev1.ConditionTrue
						}
					}

					return false
				}).WithTimeout(nhcparams.NodeRecoveryTimeout).
					WithPolling(nhcparams.PollingInterval).
					Should(BeTrue(),
						fmt.Sprintf("Node %s did not recover to Ready state", targetNode))

				klog.Infof("Node %s recovered to Ready state", targetNode)
			})

			DeferCleanup(func() {
				By("Uncordoning node if still cordoned")

				node, pullErr := nodes.Pull(APIClient, targetNode)
				if pullErr != nil {
					klog.Warningf("Failed to pull node %s for uncordon: %v", targetNode, pullErr)

					return
				}

				if node.Object.Spec.Unschedulable {
					uncordonErr := node.Uncordon()
					if uncordonErr != nil {
						klog.Warningf("Failed to uncordon node %s: %v", targetNode, uncordonErr)
					}
				}
			})

			DeferCleanup(func() {
				By("Removing pauseRequests from NHC")

				unpausePatch := []byte(`{"spec":{"pauseRequests":[]}}`)

				_, patchErr := APIClient.Resource(nhcparams.NhcGVR).Patch(
					context.TODO(),
					nhcparams.NHCResourceName,
					types.MergePatchType,
					unpausePatch,
					metav1.PatchOptions{})
				if patchErr != nil {
					klog.Warningf("Failed to unpause NHC: %v", patchErr)
				}
			})

			DeferCleanup(func() {
				By("Purging any residual SNR resources")

				snrList, listErr := APIClient.Resource(rhwaparams.SnrGVR).
					Namespace(rhwaparams.RhwaOperatorNs).
					List(context.TODO(), metav1.ListOptions{})
				if listErr != nil {
					klog.Warningf("Failed to list SNR resources during cleanup: %v", listErr)

					return
				}

				for _, snr := range snrList.Items {
					deleteErr := APIClient.Resource(rhwaparams.SnrGVR).
						Namespace(rhwaparams.RhwaOperatorNs).
						Delete(context.TODO(), snr.GetName(), metav1.DeleteOptions{})
					if deleteErr != nil {
						klog.Warningf("Failed to delete SNR %s: %v", snr.GetName(), deleteErr)
					}
				}
			})

			DeferCleanup(func() {
				By("Restoring appworker label on nodes where it was temporarily removed")

				restoreLabelPatch := []byte(
					fmt.Sprintf(`{"metadata":{"labels":{%q:""}}}`, nhcparams.AppWorkerLabel))

				for _, workerName := range unlabeledWorkers {
					_, patchErr := APIClient.K8sClient.CoreV1().Nodes().Patch(
						context.TODO(), workerName, types.MergePatchType,
						restoreLabelPatch, metav1.PatchOptions{})
					if patchErr != nil {
						klog.Warningf("Failed to restore label on node %s: %v", workerName, patchErr)
					}
				}
			})

			DeferCleanup(func() {
				By("Removing appworker label from nodes labeled by the test")

				removeLabelPatch := []byte(
					fmt.Sprintf(`{"metadata":{"labels":{%q:null}}}`, nhcparams.AppWorkerLabel))

				for _, workerName := range labeledWorkers {
					_, patchErr := APIClient.K8sClient.CoreV1().Nodes().Patch(
						context.TODO(), workerName, types.MergePatchType,
						removeLabelPatch, metav1.PatchOptions{})
					if patchErr != nil {
						klog.Warningf("Failed to remove label from node %s: %v", workerName, patchErr)
					}
				}
			})

			DeferCleanup(func() {
				By("Deleting test namespace")

				testNS := namespace.NewBuilder(APIClient, nhcparams.AppNamespace)

				if deleteErr := testNS.DeleteAndWait(nhcparams.DeletionTimeout); deleteErr != nil {
					klog.Warningf("Failed to delete test namespace: %v", deleteErr)
				}
			})
		})

		It("Step 1: Verifies initial cluster state",
			reportxml.ID("10001"), func() {
				By("Verifying all worker nodes are Ready")

				for _, workerName := range append([]string{RHWAConfig.TargetWorker}, RHWAConfig.FailoverWorkers...) {
					workerNode, err := nodes.Pull(APIClient, workerName)
					Expect(err).ToNot(HaveOccurred(),
						fmt.Sprintf("Failed to pull node %s", workerName))

					ready, err := workerNode.IsReady()
					Expect(err).ToNot(HaveOccurred(),
						fmt.Sprintf("Failed to check Ready condition for node %s", workerName))
					Expect(ready).To(BeTrue(),
						fmt.Sprintf("Node %s is not Ready", workerName))
				}

				By("Verifying MCP 'worker' is not Degraded")

				workerMCP, err := mco.Pull(APIClient, "worker")
				Expect(err).ToNot(HaveOccurred(), "Failed to pull MCP 'worker'")
				Expect(workerMCP.IsInCondition(mcfgv1.MachineConfigPoolDegraded)).To(BeFalse(),
					"MCP 'worker' is in Degraded condition")

				By("Verifying NHC reports healthy nodes")

				nhcResource, err := APIClient.Resource(nhcparams.NhcGVR).Get(
					context.TODO(), nhcparams.NHCResourceName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to get NodeHealthCheck resource")

				nhcStatus, hasStatus := nhcResource.Object["status"].(map[string]any)
				if hasStatus {
					unhealthyNodes, _ := nhcStatus["unhealthyNodes"].([]any)
					Expect(unhealthyNodes).To(BeEmpty(),
						"NHC reports unhealthy nodes before test begins")

					klog.Infof("NHC status: healthy=%v, observed=%v",
						nhcStatus["healthyNodes"], nhcStatus["observedNodes"])
				}

				By("Verifying no SelfNodeRemediation resources exist")

				snrList, err := APIClient.Resource(rhwaparams.SnrGVR).
					Namespace(rhwaparams.RhwaOperatorNs).
					List(context.TODO(), metav1.ListOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to list SNR resources")
				Expect(snrList.Items).To(BeEmpty(),
					"SelfNodeRemediation resources should not exist before test")
			})

		It("Step 2: Deploys stateful app on target node and pauses NHC",
			reportxml.ID("10002"), func() {
				By("Ensuring only target node has appworker label")

				removeLabelPatch := []byte(
					fmt.Sprintf(`{"metadata":{"labels":{%q:null}}}`, nhcparams.AppWorkerLabel))
				addLabelPatch := []byte(
					fmt.Sprintf(`{"metadata":{"labels":{%q:""}}}`, nhcparams.AppWorkerLabel))

				for _, workerName := range RHWAConfig.FailoverWorkers {
					var workerNode *nodes.Builder

					var pullErr error

					for attempt := 1; attempt <= 3; attempt++ {
						workerNode, pullErr = nodes.Pull(APIClient, workerName)
						if pullErr == nil {
							break
						}

						klog.Warningf("Failed to pull failover node %s (attempt %d/3): %v",
							workerName, attempt, pullErr)
						time.Sleep(nhcparams.PollingInterval)
					}

					if pullErr != nil {
						klog.Warningf("Giving up on failover node %s after 3 attempts", workerName)

						continue
					}

					if _, exists := workerNode.Object.Labels[nhcparams.AppWorkerLabel]; exists {
						klog.Infof("Temporarily removing %s label from failover node %s before deployment",
							nhcparams.AppWorkerLabel, workerName)

						_, patchErr := APIClient.K8sClient.CoreV1().Nodes().Patch(
							context.TODO(), workerName, types.MergePatchType,
							removeLabelPatch, metav1.PatchOptions{})
						Expect(patchErr).ToNot(HaveOccurred(),
							fmt.Sprintf("Failed to remove label from node %s", workerName))

						unlabeledWorkers = append(unlabeledWorkers, workerName)
					}
				}

				targetWorkerNode, err := nodes.Pull(APIClient, targetNode)
				Expect(err).ToNot(HaveOccurred(),
					fmt.Sprintf("Failed to pull node %s", targetNode))

				if _, exists := targetWorkerNode.Object.Labels[nhcparams.AppWorkerLabel]; !exists {
					_, patchErr := APIClient.K8sClient.CoreV1().Nodes().Patch(
						context.TODO(), targetNode, types.MergePatchType,
						addLabelPatch, metav1.PatchOptions{})
					Expect(patchErr).ToNot(HaveOccurred(),
						fmt.Sprintf("Failed to label node %s", targetNode))

					labeledWorkers = append(labeledWorkers, targetNode)
				}

				By("Creating test namespace")

				testNS := namespace.NewBuilder(APIClient, nhcparams.AppNamespace)

				_, err = testNS.Create()
				Expect(err).ToNot(HaveOccurred(), "Failed to create test namespace")

				By("Creating PersistentVolumeClaim")

				pvcBuilder := storage.NewPVCBuilder(APIClient, nhcparams.PVCName, nhcparams.AppNamespace)

				pvcBuilder, err = pvcBuilder.WithPVCAccessMode("ReadWriteOnce")
				Expect(err).ToNot(HaveOccurred(), "Failed to set PVC access mode")

				pvcBuilder, err = pvcBuilder.WithPVCCapacity(nhcparams.PVCSize)
				Expect(err).ToNot(HaveOccurred(), "Failed to set PVC capacity")

				pvcBuilder, err = pvcBuilder.WithStorageClass(RHWAConfig.StorageClass)
				Expect(err).ToNot(HaveOccurred(), "Failed to set PVC storage class")

				_, err = pvcBuilder.Create()
				Expect(err).ToNot(HaveOccurred(), "Failed to create PVC")

				By("Creating stateful app deployment")

				appLabels := map[string]string{nhcparams.AppLabelKey: nhcparams.AppLabelValue}

				containerCmd := []string{"/bin/sh", "-c",
					`echo "Starting stateful app on $(hostname)" > /data/heartbeat.log; ` +
						`while true; do echo "$(date -Iseconds) alive" >> /data/heartbeat.log; sleep 5; done`}

				container := pod.NewContainerBuilder(nhcparams.AppName, RHWAConfig.AppImage, containerCmd).
					WithVolumeMount(corev1.VolumeMount{
						Name:      nhcparams.PVCName,
						MountPath: "/data",
					})

				containerSpec, err := container.GetContainerCfg()
				Expect(err).ToNot(HaveOccurred(), "Failed to build container spec")

				containerSpec.SecurityContext = nil
				containerSpec.ReadinessProbe = &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{
							Command: []string{"cat", "/data/heartbeat.log"},
						},
					},
					InitialDelaySeconds: 5,
					PeriodSeconds:       5,
				}

				deploy := deployment.NewBuilder(
					APIClient, nhcparams.AppName, nhcparams.AppNamespace, appLabels, *containerSpec).
					WithNodeSelector(map[string]string{nhcparams.AppWorkerLabel: ""}).
					WithReplicas(int32(1)).
					WithVolume(corev1.Volume{
						Name: nhcparams.PVCName,
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: nhcparams.PVCName,
							},
						},
					})

				deploy.Definition.Spec.Strategy = appsv1.DeploymentStrategy{
					Type: appsv1.RecreateDeploymentStrategyType,
				}

				_, err = deploy.CreateAndWaitUntilReady(nhcparams.DeploymentTimeout)
				Expect(err).ToNot(HaveOccurred(), "Failed to create stateful app deployment")

				By("Verifying app pod is running on the target node")

				appPods, err := pod.List(APIClient, nhcparams.AppNamespace, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", nhcparams.AppLabelKey, nhcparams.AppLabelValue),
					FieldSelector: "status.phase=Running",
				})
				Expect(err).ToNot(HaveOccurred(), "Failed to list app pods")
				Expect(appPods).To(HaveLen(1), "Expected exactly 1 running app pod")

				targetNode = appPods[0].Object.Spec.NodeName
				klog.Infof("Stateful app is running on node %s", targetNode)

				Expect(targetNode).To(Equal(RHWAConfig.TargetWorker),
					"App pod should be pinned to the configured target node")

				By("Labeling failover worker nodes")

				addLabelPatchFO := []byte(
					fmt.Sprintf(`{"metadata":{"labels":{%q:""}}}`, nhcparams.AppWorkerLabel))

				for _, workerName := range RHWAConfig.FailoverWorkers {
					if workerName == targetNode {
						continue
					}

					workerNode, pullErr := nodes.Pull(APIClient, workerName)
					Expect(pullErr).ToNot(HaveOccurred(),
						fmt.Sprintf("Failed to pull node %s", workerName))

					if _, exists := workerNode.Object.Labels[nhcparams.AppWorkerLabel]; exists {
						klog.Infof("Node %s already has label %s, skipping", workerName, nhcparams.AppWorkerLabel)

						continue
					}

					_, patchErr := APIClient.K8sClient.CoreV1().Nodes().Patch(
						context.TODO(), workerName, types.MergePatchType,
						addLabelPatchFO, metav1.PatchOptions{})
					Expect(patchErr).ToNot(HaveOccurred(),
						fmt.Sprintf("Failed to label node %s", workerName))

					labeledWorkers = append(labeledWorkers, workerName)
				}

				By("Pausing NHC remediation via pauseRequests")

				pausePatch := []byte(fmt.Sprintf(
					`{"spec":{"pauseRequests":[%q]}}`,
					nhcparams.NHCPauseReason))

				_, err = APIClient.Resource(nhcparams.NhcGVR).Patch(
					context.TODO(),
					nhcparams.NHCResourceName,
					types.MergePatchType,
					pausePatch,
					metav1.PatchOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to pause NHC")

				By("Verifying pauseRequests was set")

				nhcResource, err := APIClient.Resource(nhcparams.NhcGVR).Get(
					context.TODO(), nhcparams.NHCResourceName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to get NHC resource")

				nhcSpec, specOk := nhcResource.Object["spec"].(map[string]any)
				Expect(specOk).To(BeTrue(), "NHC resource has no spec")

				pauseRequests, pauseOk := nhcSpec["pauseRequests"].([]any)
				Expect(pauseOk).To(BeTrue(), "pauseRequests not found in NHC spec")
				Expect(pauseRequests).To(HaveLen(1),
					"Expected exactly 1 pause request")

				klog.Infof("NHC paused with pauseRequests: %v", pauseRequests)
			})

		It("Step 3: Cordons and drains the target node",
			reportxml.ID("10003"), func() {
				By("Cordoning the target node")

				targetNodeObj, err := nodes.Pull(APIClient, targetNode)
				Expect(err).ToNot(HaveOccurred(),
					fmt.Sprintf("Failed to pull node %s", targetNode))

				err = targetNodeObj.Cordon()
				Expect(err).ToNot(HaveOccurred(),
					fmt.Sprintf("Failed to cordon node %s", targetNode))

				klog.Infof("Node %s cordoned", targetNode)

				By("Draining the target node")

				targetNodeObj, err = nodes.Pull(APIClient, targetNode)
				Expect(err).ToNot(HaveOccurred(),
					fmt.Sprintf("Failed to re-pull node %s for drain", targetNode))

				err = targetNodeObj.Drain()
				Expect(err).ToNot(HaveOccurred(),
					fmt.Sprintf("Failed to drain node %s", targetNode))

				klog.Infof("Node %s drained", targetNode)
			})

		It("Step 4: Verifies NHC remains paused and does not create SNR",
			reportxml.ID("10004"), func() {
				By("Observing NHC behaviour over time to confirm no remediation triggers")

				Consistently(func() bool {
					nhcResource, nhcErr := APIClient.Resource(nhcparams.NhcGVR).Get(
						context.TODO(), nhcparams.NHCResourceName, metav1.GetOptions{})
					if nhcErr != nil {
						klog.Warningf("  observing NHC: error getting resource: %v", nhcErr)

						return true
					}

					nhcSpec, specOk := nhcResource.Object["spec"].(map[string]any)
					if !specOk {
						klog.Warningf("  observing NHC: no spec found")

						return true
					}

					pauseRequests, _ := nhcSpec["pauseRequests"].([]any)
					if len(pauseRequests) == 0 {
						klog.Errorf("  observing NHC: pauseRequests unexpectedly empty")

						return false
					}

					snrList, listErr := APIClient.Resource(rhwaparams.SnrGVR).
						Namespace(rhwaparams.RhwaOperatorNs).
						List(context.TODO(), metav1.ListOptions{})
					if listErr != nil {
						klog.Warningf("  observing NHC: error listing SNR: %v", listErr)

						return true
					}

					for _, snr := range snrList.Items {
						annotations := snr.GetAnnotations()
						if annotations["remediation.medik8s.io/node-name"] == targetNode {
							klog.Errorf("  observing NHC: UNEXPECTED SNR %s created for %s while NHC is paused",
								snr.GetName(), targetNode)

							return false
						}
					}

					klog.Infof("  observing NHC: paused with %d pauseRequests, no SNR for %s",
						len(pauseRequests), targetNode)

					return true
				}).WithTimeout(nhcparams.NHCPauseObserveTimeout).
					WithPolling(nhcparams.PollingInterval).
					Should(BeTrue(),
						"NHC triggered remediation while paused")
			})

		It("Step 5: Verifies SNR has not fenced the node",
			reportxml.ID("10005"), func() {
				By("Verifying no out-of-service taint on the target node")

				targetNodeObj, err := nodes.Pull(APIClient, targetNode)
				Expect(err).ToNot(HaveOccurred(),
					fmt.Sprintf("Failed to pull node %s", targetNode))

				for _, taint := range targetNodeObj.Object.Spec.Taints {
					Expect(taint.Key).ToNot(Equal(nhcparams.OutOfServiceTaintKey),
						fmt.Sprintf("Node %s has out-of-service taint — SNR fenced it unexpectedly", targetNode))
				}

				By("Verifying no SelfNodeRemediation resources exist")

				snrList, err := APIClient.Resource(rhwaparams.SnrGVR).
					Namespace(rhwaparams.RhwaOperatorNs).
					List(context.TODO(), metav1.ListOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to list SNR resources")
				Expect(snrList.Items).To(BeEmpty(),
					"SelfNodeRemediation resources exist — SNR acted unexpectedly")
			})

		It("Step 6: Verifies workloads migrated to a failover worker",
			reportxml.ID("10006"), func() {
				By("Waiting for app pod to be Ready on a different node")

				Eventually(func() bool {
					appPods, listErr := pod.List(APIClient, nhcparams.AppNamespace, metav1.ListOptions{
						LabelSelector: fmt.Sprintf("%s=%s", nhcparams.AppLabelKey, nhcparams.AppLabelValue),
					})
					if listErr != nil {
						klog.Infof("  polling reschedule: error listing app pods: %v", listErr)

						return false
					}

					for idx := range appPods {
						appPod := appPods[idx]
						podReady := isPodReady(appPod)
						klog.Infof("  polling reschedule: pod %s phase=%s ready=%v node=%s",
							appPod.Object.Name, appPod.Object.Status.Phase, podReady, appPod.Object.Spec.NodeName)

						if podReady && appPod.Object.Spec.NodeName != targetNode {
							klog.Infof("  polling reschedule: app rescheduled and Ready on %s",
								appPod.Object.Spec.NodeName)

							return true
						}
					}

					return false
				}).WithTimeout(nhcparams.RescheduleTimeout).
					WithPolling(nhcparams.PollingInterval).
					Should(BeTrue(), "App pod was not rescheduled to a healthy node after drain")

				By("Verifying PVC is still Bound")

				pvcList, err := storage.ListPVC(APIClient, nhcparams.AppNamespace, metav1.ListOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to list PVCs")

				var appPVC *storage.PVCBuilder

				for idx := range pvcList {
					if pvcList[idx].Object.Name == nhcparams.PVCName {
						appPVC = pvcList[idx]

						break
					}
				}

				Expect(appPVC).ToNot(BeNil(), "PVC not found")
				Expect(appPVC.Object.Status.Phase).To(Equal(corev1.ClaimBound),
					"PVC is not Bound")

				klog.Infof("PVC %s is Bound", nhcparams.PVCName)

				By("Verifying no evictable workloads remain on the target node")

				remainingPods, err := pod.List(APIClient, "", metav1.ListOptions{
					FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase=Running", targetNode),
				})
				Expect(err).ToNot(HaveOccurred(), "Failed to list pods on target node")

				for _, p := range remainingPods {
					ownerRefs := p.Object.OwnerReferences
					isDaemonSet := false

					for _, ref := range ownerRefs {
						if ref.Kind == "DaemonSet" {
							isDaemonSet = true

							break
						}
					}

					if !isDaemonSet {
						klog.Warningf("Non-DaemonSet pod %s/%s still running on drained node %s",
							p.Object.Namespace, p.Object.Name, targetNode)
					}
				}
			})

		It("Step 7: Removes the worker node",
			reportxml.ID("10007"), func() {
				By("Deleting the node object from the cluster")

				targetNodeObj, err := nodes.Pull(APIClient, targetNode)
				Expect(err).ToNot(HaveOccurred(),
					fmt.Sprintf("Failed to pull node %s", targetNode))

				err = targetNodeObj.Delete()
				Expect(err).ToNot(HaveOccurred(),
					fmt.Sprintf("Failed to delete node %s", targetNode))

				By("Verifying node object is deleted")

				Eventually(func() bool {
					_, pullErr := nodes.Pull(APIClient, targetNode)

					return pullErr != nil
				}).WithTimeout(nhcparams.NodeDeletionTimeout).
					WithPolling(nhcparams.PollingInterval).
					Should(BeTrue(),
						fmt.Sprintf("Node %s was not deleted from the cluster", targetNode))

				klog.Infof("Node %s removed from the cluster", targetNode)

				By("Powering off the node via BMC")

				err = bmcClient.SystemPowerOff()
				Expect(err).ToNot(HaveOccurred(), "Failed to power off node via BMC")

				klog.Infof("Node %s powered off via BMC", targetNode)
			})

		It("Step 8: Verifies post-removal cluster health",
			reportxml.ID("10008"), func() {
				By("Verifying remaining nodes are Ready")

				for _, workerName := range RHWAConfig.FailoverWorkers {
					if workerName == targetNode {
						continue
					}

					workerNode, err := nodes.Pull(APIClient, workerName)
					Expect(err).ToNot(HaveOccurred(),
						fmt.Sprintf("Failed to pull node %s", workerName))

					ready, err := workerNode.IsReady()
					Expect(err).ToNot(HaveOccurred(),
						fmt.Sprintf("Failed to check Ready condition for node %s", workerName))
					Expect(ready).To(BeTrue(),
						fmt.Sprintf("Node %s is not Ready", workerName))
				}

				By("Verifying MCP 'worker' is healthy")

				Eventually(func() bool {
					workerMCP, mcpErr := mco.Pull(APIClient, "worker")
					if mcpErr != nil {
						klog.Infof("  polling MCP: error pulling: %v", mcpErr)

						return false
					}

					isDegraded := workerMCP.IsInCondition(mcfgv1.MachineConfigPoolDegraded)
					if isDegraded {
						klog.Infof("  polling MCP: 'worker' is Degraded — waiting for reconciliation")

						return false
					}

					klog.Infof("  polling MCP: 'worker' is healthy")

					return true
				}).WithTimeout(nhcparams.MCPHealthTimeout).
					WithPolling(nhcparams.PollingInterval).
					Should(BeTrue(), "MCP 'worker' is degraded after node removal")

				By("Verifying stateful app is still running")

				appPods, err := pod.List(APIClient, nhcparams.AppNamespace, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", nhcparams.AppLabelKey, nhcparams.AppLabelValue),
				})
				Expect(err).ToNot(HaveOccurred(), "Failed to list app pods")

				runningPods := filterRunningPods(appPods)
				Expect(runningPods).To(HaveLen(1), "Expected exactly 1 running app pod")
				Expect(runningPods[0].Object.Spec.NodeName).ToNot(Equal(targetNode),
					"App pod should not be on the removed node")

				klog.Infof("App pod is running on %s", runningPods[0].Object.Spec.NodeName)

				By("Verifying no SelfNodeRemediation resources were created")

				snrList, err := APIClient.Resource(rhwaparams.SnrGVR).
					Namespace(rhwaparams.RhwaOperatorNs).
					List(context.TODO(), metav1.ListOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to list SNR resources")
				Expect(snrList.Items).To(BeEmpty(),
					"SelfNodeRemediation resources exist after node removal — NHC acted unexpectedly")

				By("Verifying NHC still has pauseRequests")

				nhcResource, err := APIClient.Resource(nhcparams.NhcGVR).Get(
					context.TODO(), nhcparams.NHCResourceName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to get NHC resource")

				nhcSpec, specOk := nhcResource.Object["spec"].(map[string]any)
				Expect(specOk).To(BeTrue(), "NHC resource has no spec")

				pauseRequests, _ := nhcSpec["pauseRequests"].([]any)
				Expect(pauseRequests).ToNot(BeEmpty(),
					"NHC pauseRequests unexpectedly cleared during node removal")
			})

		It("Step 9: Cleans up and unpauses NHC",
			reportxml.ID("10009"), func() {
				By("Purging any residual SNR resources")

				snrList, err := APIClient.Resource(rhwaparams.SnrGVR).
					Namespace(rhwaparams.RhwaOperatorNs).
					List(context.TODO(), metav1.ListOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to list SNR resources")

				for _, snr := range snrList.Items {
					deleteErr := APIClient.Resource(rhwaparams.SnrGVR).
						Namespace(rhwaparams.RhwaOperatorNs).
						Delete(context.TODO(), snr.GetName(), metav1.DeleteOptions{})
					if deleteErr != nil {
						klog.Warningf("Failed to delete SNR %s: %v", snr.GetName(), deleteErr)
					}
				}

				By("Removing pauseRequests from NHC")

				unpausePatch := []byte(`{"spec":{"pauseRequests":[]}}`)

				_, err = APIClient.Resource(nhcparams.NhcGVR).Patch(
					context.TODO(),
					nhcparams.NHCResourceName,
					types.MergePatchType,
					unpausePatch,
					metav1.PatchOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to unpause NHC")

				By("Verifying pauseRequests was cleared")

				nhcResource, err := APIClient.Resource(nhcparams.NhcGVR).Get(
					context.TODO(), nhcparams.NHCResourceName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to get NHC resource")

				nhcSpec, specOk := nhcResource.Object["spec"].(map[string]any)
				Expect(specOk).To(BeTrue(), "NHC resource has no spec")

				pauseRequests, _ := nhcSpec["pauseRequests"].([]any)
				Expect(pauseRequests).To(BeEmpty(),
					"NHC pauseRequests was not cleared")

				klog.Infof("NHC unpaused successfully")

				By("Verifying NHC is actively observing the updated worker pool")

				Eventually(func() bool {
					nhc, nhcErr := APIClient.Resource(nhcparams.NhcGVR).Get(
						context.TODO(), nhcparams.NHCResourceName, metav1.GetOptions{})
					if nhcErr != nil {
						klog.Infof("  polling NHC status: %v", nhcErr)

						return false
					}

					nhcStatus, hasStatus := nhc.Object["status"].(map[string]any)
					if !hasStatus {
						klog.Infof("  polling NHC status: no status yet")

						return false
					}

					unhealthyNodes, _ := nhcStatus["unhealthyNodes"].([]any)
					if len(unhealthyNodes) > 0 {
						klog.Infof("  polling NHC status: %d unhealthy nodes — waiting for reconciliation",
							len(unhealthyNodes))

						return false
					}

					klog.Infof("NHC post-cleanup: healthy=%v, observed=%v",
						nhcStatus["healthyNodes"], nhcStatus["observedNodes"])

					return true
				}).WithTimeout(nhcparams.NHCObserveTimeout).
					WithPolling(nhcparams.PollingInterval).
					Should(BeTrue(), "NHC still reports unhealthy nodes after cleanup")
			})
	})
