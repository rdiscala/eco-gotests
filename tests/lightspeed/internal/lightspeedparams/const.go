package lightspeedparams

import (
	"time"
)

const (
	// Label represents the lightspeed label that can be used for test cases selection.
	Label = "lightspeed"

	// OLSNamespace is the namespace where the OpenShift Lightspeed operator and workloads run.
	OLSNamespace = "openshift-lightspeed"

	// OperatorSubscriptionName is the name of the OLS operator Subscription.
	OperatorSubscriptionName = "lightspeed-operator"

	// OperatorGroupName is the name of the OLS OperatorGroup.
	OperatorGroupName = "lightspeed-operator"

	// OperatorDeploymentName is the OLS operator controller manager deployment name.
	OperatorDeploymentName = "lightspeed-operator-controller-manager"

	// CSVNamePattern is the name pattern used to locate the OLS ClusterServiceVersion.
	CSVNamePattern = "lightspeed-operator"

	// CatalogSourceName is the disconnected catalog source the Subscription must reference.
	CatalogSourceName = "redhat-operators-disconnected"

	// OLSConfigName is the name of the singleton OLSConfig CR.
	OLSConfigName = "cluster"

	// QueryAccessClusterRole is the ClusterRole that grants OLS query access.
	QueryAccessClusterRole = "lightspeed-operator-query-access"

	// QueryAccessBindingName is the ClusterRoleBinding granting query access to the authorized group.
	QueryAccessBindingName = "lightspeed-operator-query-access-binding"

	// AuthorizedUsersGroup is the group bound to the OLS query-access ClusterRole.
	AuthorizedUsersGroup = "lightspeed-authorized-users"

	// PrometheusRuleName is the name of the OLS PrometheusRule CR.
	PrometheusRuleName = "lightspeed-alerts"

	// ClusterMonitoringLabel is the namespace label enabling cluster monitoring scraping.
	ClusterMonitoringLabel = "openshift.io/cluster-monitoring"

	// DefaultTimeout represents the default timeout for OLS operations.
	DefaultTimeout = 300 * time.Second
)
