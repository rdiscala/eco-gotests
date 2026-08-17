package tests

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/namespace"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/olm"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/rbac"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	operatorsv1alpha1 "github.com/rh-ecosystem-edge/eco-goinfra/pkg/schemes/olm/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	//nolint:staticcheck
	. "github.com/rh-ecosystem-edge/eco-gotests/tests/lightspeed/internal/lightspeedinittools"
	"github.com/rh-ecosystem-edge/eco-gotests/tests/lightspeed/internal/lightspeedparams"
)

var _ = Describe(
	"OpenShift Lightspeed RDS reference configuration",
	Ordered,
	ContinueOnFailure,
	Label(lightspeedparams.Label), func() {
		// Scenario 1 / Scenario 2: operator installs successfully (Hub via ArgoCD Automatic,
		// Core via PolicyGenerator Manual). AC #1, #2, #10.
		Context("Operator installation", Label("install"), func() {
			It("Namespace exists with the cluster-monitoring label", reportxml.ID("CNF-22695-01"), func() {
				By("Pulling the openshift-lightspeed namespace")

				nsBuilder, err := namespace.Pull(APIClient, lightspeedparams.OLSNamespace)
				Expect(err).ToNot(HaveOccurred(), "Failed to pull %s namespace", lightspeedparams.OLSNamespace)

				By("Verifying the cluster-monitoring label is set to true")

				labels := nsBuilder.Object.Labels
				Expect(labels).To(HaveKeyWithValue(lightspeedparams.ClusterMonitoringLabel, "true"),
					"Namespace %s must carry label %s=true",
					lightspeedparams.OLSNamespace, lightspeedparams.ClusterMonitoringLabel)
			})

			It("Subscription references the disconnected catalog source", reportxml.ID("CNF-22695-02"), func() {
				By("Pulling the lightspeed-operator Subscription")

				subBuilder, err := olm.PullSubscription(
					APIClient, lightspeedparams.OperatorSubscriptionName, lightspeedparams.OLSNamespace)
				Expect(err).ToNot(HaveOccurred(), "Failed to pull OLS Subscription")

				By("Verifying the catalog source is redhat-operators-disconnected")

				Expect(subBuilder.Object.Spec.CatalogSource).To(Equal(lightspeedparams.CatalogSourceName),
					"Subscription must reference the disconnected catalog source")
				Expect(subBuilder.Object.Spec.CatalogSourceNamespace).To(Equal("openshift-marketplace"),
					"Subscription catalog source namespace must be openshift-marketplace")
			})

			It("Subscription installPlanApproval matches an RDS profile", reportxml.ID("CNF-22695-03"), func() {
				By("Pulling the lightspeed-operator Subscription")

				subBuilder, err := olm.PullSubscription(
					APIClient, lightspeedparams.OperatorSubscriptionName, lightspeedparams.OLSNamespace)
				Expect(err).ToNot(HaveOccurred(), "Failed to pull OLS Subscription")

				approval := subBuilder.Object.Spec.InstallPlanApproval

				By("Verifying installPlanApproval is Automatic (Hub) or Manual (Core)")

				Expect(approval).To(BeElementOf(
					operatorsv1alpha1.ApprovalAutomatic, operatorsv1alpha1.ApprovalManual),
					"installPlanApproval must be Automatic (Hub RDS) or Manual (Core RDS)")

				// On Core (Manual), an InstallPlan is created and must be approved for the
				// operator to install. Verify at least one InstallPlan exists in that case.
				if approval == operatorsv1alpha1.ApprovalManual {
					By("Verifying an InstallPlan exists for the Manual (Core) profile")

					installPlans, err := olm.ListInstallPlan(APIClient, lightspeedparams.OLSNamespace)
					Expect(err).ToNot(HaveOccurred(), "Failed to list InstallPlans")
					Expect(installPlans).ToNot(BeEmpty(),
						"Manual approval profile must have at least one InstallPlan")
				}
			})

			It("Operator CSV reaches the Succeeded phase", reportxml.ID("CNF-22695-04"), func() {
				By("Listing OLS ClusterServiceVersions")

				csvs, err := olm.ListClusterServiceVersionWithNamePattern(
					APIClient, lightspeedparams.CSVNamePattern, lightspeedparams.OLSNamespace)
				Expect(err).ToNot(HaveOccurred(), "Failed to list OLS ClusterServiceVersions")
				Expect(csvs).ToNot(BeEmpty(), "At least one OLS ClusterServiceVersion should be found")

				By("Verifying the CSV phase is Succeeded")

				Expect(string(csvs[0].Object.Status.Phase)).To(Equal("Succeeded"),
					"OLS operator CSV must be in the Succeeded phase")
			})

			It("Operator controller-manager deployment is Ready", reportxml.ID("CNF-22695-05"), func() {
				By("Pulling the OLS controller-manager deployment")

				olsDeployment, err := deployment.Pull(
					APIClient, lightspeedparams.OperatorDeploymentName, lightspeedparams.OLSNamespace)
				Expect(err).ToNot(HaveOccurred(), "Failed to pull OLS operator deployment")

				By("Verifying the deployment is Ready")

				Expect(olsDeployment.IsReady(lightspeedparams.DefaultTimeout)).To(BeTrue(),
					"OLS operator deployment is not Ready")
			})
		})

		// Scenario 3: OLSConfig defaults. AC #8, #14.
		Context("OLSConfig privacy and introspection defaults", Label("config"), func() {
			var olsConfig *unstructured.Unstructured

			BeforeEach(func() {
				By("Reading the OLSConfig CR via the dynamic client")

				var err error

				olsConfig, err = APIClient.Resource(lightspeedparams.OLSConfigGVR).
					Get(context.TODO(), lightspeedparams.OLSConfigName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to get OLSConfig %s", lightspeedparams.OLSConfigName)
			})

			It("has introspection disabled by default", reportxml.ID("CNF-22695-06"), func() {
				introspection, found, err := unstructured.NestedBool(
					olsConfig.Object, "spec", "ols", "introspectionEnabled")
				Expect(err).ToNot(HaveOccurred(), "Failed to read introspectionEnabled")
				Expect(found).To(BeTrue(), "spec.ols.introspectionEnabled must be present")
				Expect(introspection).To(BeFalse(), "introspectionEnabled must default to false")
			})

			It("has user data collection disabled by default", reportxml.ID("CNF-22695-07"), func() {
				feedbackDisabled, found, err := unstructured.NestedBool(
					olsConfig.Object, "spec", "ols", "userDataCollection", "feedbackDisabled")
				Expect(err).ToNot(HaveOccurred(), "Failed to read feedbackDisabled")
				Expect(found).To(BeTrue(), "feedbackDisabled must be present")
				Expect(feedbackDisabled).To(BeTrue(), "feedbackDisabled must default to true")

				transcriptsDisabled, found, err := unstructured.NestedBool(
					olsConfig.Object, "spec", "ols", "userDataCollection", "transcriptsDisabled")
				Expect(err).ToNot(HaveOccurred(), "Failed to read transcriptsDisabled")
				Expect(found).To(BeTrue(), "transcriptsDisabled must be present")
				Expect(transcriptsDisabled).To(BeTrue(), "transcriptsDisabled must default to true")
			})

			It("references LLM credentials by secret name only", reportxml.ID("CNF-22695-08"), func() {
				providers, found, err := unstructured.NestedSlice(
					olsConfig.Object, "spec", "llm", "providers")
				Expect(err).ToNot(HaveOccurred(), "Failed to read llm.providers")
				Expect(found).To(BeTrue(), "spec.llm.providers must be present")
				Expect(providers).ToNot(BeEmpty(), "at least one LLM provider must be configured")

				// Every provider must reference credentials by secret name, never embed them inline.
				for idx, raw := range providers {
					provider, ok := raw.(map[string]interface{})
					Expect(ok).To(BeTrue(), "provider[%d] must be a map", idx)

					secretName, found, err := unstructured.NestedString(
						provider, "credentialsSecretRef", "name")
					Expect(err).ToNot(HaveOccurred(), "Failed to read credentialsSecretRef.name")
					Expect(found).To(BeTrue(), "provider[%d] must reference a credentials secret", idx)
					Expect(secretName).ToNot(BeEmpty(),
						"provider[%d] credentialsSecretRef.name must not be empty", idx)
				}
			})
		})

		// Scenario 4: RBAC restricts OLS access. AC #3, #13.
		Context("RBAC query-access controls", Label("rbac"), func() {
			It("binds the query-access ClusterRole to the authorized group", reportxml.ID("CNF-22695-09"), func() {
				By("Pulling the OLS query-access ClusterRoleBinding")

				crbBuilder, err := rbac.PullClusterRoleBinding(APIClient, lightspeedparams.QueryAccessBindingName)
				Expect(err).ToNot(HaveOccurred(), "Failed to pull OLS ClusterRoleBinding")

				By("Verifying the roleRef targets the query-access ClusterRole")

				Expect(crbBuilder.Object.RoleRef.Kind).To(Equal("ClusterRole"),
					"roleRef must reference a ClusterRole")
				Expect(crbBuilder.Object.RoleRef.Name).To(Equal(lightspeedparams.QueryAccessClusterRole),
					"roleRef must reference the query-access ClusterRole")

				By("Verifying the binding targets the authorized-users group")

				var boundToGroup bool

				for _, subject := range crbBuilder.Object.Subjects {
					if subject.Kind == "Group" && subject.Name == lightspeedparams.AuthorizedUsersGroup {
						boundToGroup = true

						break
					}
				}

				Expect(boundToGroup).To(BeTrue(),
					"ClusterRoleBinding must bind group %s", lightspeedparams.AuthorizedUsersGroup)
			})
		})

		// Scenario 5: Namespace monitoring label (also covered in install context for completeness). AC #15.
		Context("Observability", Label("observability"), func() {
			It("namespace has the cluster-monitoring label", reportxml.ID("CNF-22695-10"), func() {
				nsBuilder, err := namespace.Pull(APIClient, lightspeedparams.OLSNamespace)
				Expect(err).ToNot(HaveOccurred(), "Failed to pull %s namespace", lightspeedparams.OLSNamespace)

				Expect(nsBuilder.Object.Labels).To(
					HaveKeyWithValue(lightspeedparams.ClusterMonitoringLabel, "true"),
					"Namespace must enable cluster monitoring for Prometheus scraping")
			})

			// Scenario 6: PrometheusRules alerts. AC #16.
			It("defines the 5 reference PrometheusRule alerts", reportxml.ID("CNF-22695-11"), func() {
				By("Reading the OLS PrometheusRule via the dynamic client")

				promRule, err := APIClient.Resource(lightspeedparams.PrometheusRuleGVR).
					Namespace(lightspeedparams.OLSNamespace).
					Get(context.TODO(), lightspeedparams.PrometheusRuleName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to get OLS PrometheusRule")

				By("Collecting all defined alert names")

				groups, found, err := unstructured.NestedSlice(promRule.Object, "spec", "groups")
				Expect(err).ToNot(HaveOccurred(), "Failed to read spec.groups")
				Expect(found).To(BeTrue(), "spec.groups must be present")

				definedAlerts := map[string]bool{}

				for _, rawGroup := range groups {
					group, ok := rawGroup.(map[string]interface{})
					Expect(ok).To(BeTrue(), "group entry must be a map")

					rules, found, err := unstructured.NestedSlice(group, "rules")
					Expect(err).ToNot(HaveOccurred(), "Failed to read group rules")

					if !found {
						continue
					}

					for _, rawRule := range rules {
						rule, ok := rawRule.(map[string]interface{})
						Expect(ok).To(BeTrue(), "rule entry must be a map")

						if alertName, ok := rule["alert"].(string); ok {
							definedAlerts[alertName] = true
						}
					}
				}

				By("Verifying every expected alert is present")

				for _, expected := range lightspeedparams.ExpectedAlerts {
					Expect(definedAlerts).To(HaveKey(expected),
						"PrometheusRule must define the %s alert", expected)
				}
			})
		})

		// Scenario 9 / 14 / 15: workload characteristics of the OLS pods. AC #9, #19, #20.
		Context("Workload characteristics", Label("workload"), func() {
			var olsPods []*pod.Builder

			BeforeEach(func() {
				By("Listing pods in the OLS namespace")

				var err error

				olsPods, err = pod.List(APIClient, lightspeedparams.OLSNamespace, metav1.ListOptions{})
				Expect(err).ToNot(HaveOccurred(), "Failed to list OLS pods")
				Expect(olsPods).ToNot(BeEmpty(), "at least one OLS pod must be running")
			})

			It("runs burstable with no workload-partitioning annotations", reportxml.ID("CNF-22695-12"), func() {
				for _, olsPod := range olsPods {
					By(fmt.Sprintf("Checking pod %s has no workload-partitioning annotations", olsPod.Object.Name))

					for annotation := range olsPod.Object.Annotations {
						Expect(annotation).ToNot(Equal("target.workload.openshift.io/management"),
							"OLS pod %s must not carry workload-partitioning annotations", olsPod.Object.Name)
						Expect(annotation).ToNot(HavePrefix("resources.workload.openshift.io/"),
							"OLS pod %s must not carry workload-partitioning resource annotations", olsPod.Object.Name)
					}

					By(fmt.Sprintf("Checking pod %s QoS class is not Guaranteed", olsPod.Object.Name))

					Expect(olsPod.Object.Status.QOSClass).ToNot(Equal(corev1.PodQOSGuaranteed),
						"OLS pod %s must run burstable on user cores, not Guaranteed", olsPod.Object.Name)
				}
			})

			It("uses ephemeral emptyDir storage for PostgreSQL (no PVCs)", reportxml.ID("CNF-22695-13"), func() {
				// AC #20: PostgreSQL uses emptyDir; OLS creates no PersistentVolumeClaims.
				for _, olsPod := range olsPods {
					for _, volume := range olsPod.Object.Spec.Volumes {
						Expect(volume.PersistentVolumeClaim).To(BeNil(),
							"OLS pod %s must not mount a PersistentVolumeClaim (storage is ephemeral)",
							olsPod.Object.Name)
					}
				}
			})
		})
	})

// The following scenarios from TEST_PLAN.md are validated outside of a live-cluster Ginkgo run
// and are recorded here so coverage is explicit. They depend on tooling or artifacts that are
// not available to the in-cluster test process:
//
//   - Scenario 7 & 8 (kube-compare validation, AC #4): executed with the `oc cluster-compare`
//     plugin against the telco-reference kube-compare manifests from the bastion. See
//     TEST_PLAN.md; automate under a bastion-side harness once the plugin is provisioned.
//   - Scenario 10 & 11 (Hub example overlays / kustomization inclusion, AC #5, #6, #7):
//     validated by `kustomize build` against the telco-reference checkout in CI, not against a
//     running cluster.
//   - Scenario 12 (IPv6-only not supported, AC #17): documentation-level limitation; verified by
//     the documentation-consistency check.
//   - Scenario 13 (Hub backup/recovery / OADP scope, AC #18): validated on a Hub with OADP
//     configured; out of scope for the base install suite.
//   - Scenario 16 (documentation consistency, AC #11, #12): verified against the
//     reference-design-specifications repository, not the cluster.
//
// Scenarios 7, 8, 10, 11 and 16 are tracked for implementation as a separate repo/bastion-side
// suite once the kube-compare plugin and a telco-reference checkout are available to the test runner.
