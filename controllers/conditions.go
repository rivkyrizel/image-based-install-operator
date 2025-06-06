package controllers

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/image-based-install-operator/api/v1alpha1"
)

func findCondition(conditions []hivev1.ClusterInstallCondition, condType hivev1.ClusterInstallConditionType) *hivev1.ClusterInstallCondition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

func setClusterInstallCondition(conditions *[]hivev1.ClusterInstallCondition, newCondition hivev1.ClusterInstallCondition) bool {
	if conditions == nil {
		return false
	}

	now := metav1.NewTime(time.Now())
	existingCondition := findCondition(*conditions, newCondition.Type)
	if existingCondition == nil {
		newCondition.LastTransitionTime = now
		newCondition.LastProbeTime = now
		*conditions = append(*conditions, newCondition)
		return true
	}

	if existingCondition != nil &&
		existingCondition.Status == newCondition.Status &&
		existingCondition.Reason == newCondition.Reason &&
		existingCondition.Message == newCondition.Message {
		return false
	}

	if existingCondition.Status != newCondition.Status {
		existingCondition.Status = newCondition.Status
		existingCondition.LastTransitionTime = now
	}

	existingCondition.LastProbeTime = now
	existingCondition.Reason = newCondition.Reason
	existingCondition.Message = newCondition.Message
	return true
}

func (r *ImageClusterInstallReconciler) initializeConditions(ctx context.Context, ici *v1alpha1.ImageClusterInstall) error {
	initialConditions := []hivev1.ClusterInstallCondition{
		{
			Type:    hivev1.ClusterInstallRequirementsMet,
			Status:  corev1.ConditionUnknown,
			Reason:  "Unknown",
			Message: "Unknown",
		},
		{
			Type:    hivev1.ClusterInstallCompleted,
			Status:  corev1.ConditionUnknown,
			Reason:  "Unknown",
			Message: "Unknown",
		},
		{
			Type:    hivev1.ClusterInstallFailed,
			Status:  corev1.ConditionUnknown,
			Reason:  "Unknown",
			Message: "Unknown",
		},
		{
			Type:    hivev1.ClusterInstallStopped,
			Status:  corev1.ConditionUnknown,
			Reason:  "Unknown",
			Message: "Unknown",
		},
	}

	patch := client.MergeFrom(ici.DeepCopy())
	needToPatch := false
	for _, cond := range initialConditions {
		// only set the initial status if the condition doesn't exist already
		if findCondition(ici.Status.Conditions, cond.Type) == nil {
			if setClusterInstallCondition(&ici.Status.Conditions, cond) {
				needToPatch = true
			}
		}
	}
	if !needToPatch {
		return nil
	}

	r.Log.Info("Initializing conditions")
	return r.Status().Patch(ctx, ici, patch)
}

func (r *ImageClusterInstallReconciler) setRequirementsMetCondition(ctx context.Context, ici *v1alpha1.ImageClusterInstall,
	status corev1.ConditionStatus, reason, msg string) {
	cond := hivev1.ClusterInstallCondition{
		Type:    hivev1.ClusterInstallRequirementsMet,
		Status:  status,
		Reason:  reason,
		Message: msg,
	}
	patch := client.MergeFrom(ici.DeepCopy())
	if updated := setClusterInstallCondition(&ici.Status.Conditions, cond); !updated {
		return
	}
	r.Log.Infof("Setting requirements met condition, status: %s, reason: %s, message: %s", cond.Status, cond.Reason, cond.Message)
	updateErr := r.Status().Patch(ctx, ici, patch)
	if updateErr != nil {
		r.Log.WithError(updateErr).Error("failed to update requirements met condition")
	}
}

func installationTimedout(ici *v1alpha1.ImageClusterInstall) bool {
	cond := findCondition(ici.Status.Conditions, hivev1.ClusterInstallFailed)
	return cond != nil && cond.Status == corev1.ConditionTrue && cond.Reason == v1alpha1.InstallTimedoutReason
}

func InstallationCompleted(ici *v1alpha1.ImageClusterInstall) bool {
	cond := findCondition(ici.Status.Conditions, hivev1.ClusterInstallCompleted)
	return cond != nil && cond.Status == corev1.ConditionTrue
}

func (r *ImageClusterInstallMonitor) setClusterInstallingConditions(ctx context.Context, ici *v1alpha1.ImageClusterInstall, message string) error {
	patch := client.MergeFrom(ici.DeepCopy())
	completedUpdated := setClusterInstallCondition(&ici.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hivev1.ClusterInstallCompleted,
		Status:  corev1.ConditionFalse,
		Reason:  v1alpha1.InstallInProgressReason,
		Message: v1alpha1.InstallInProgressMessage,
	})
	stoppedUpdated := setClusterInstallCondition(&ici.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hivev1.ClusterInstallStopped,
		Status:  corev1.ConditionFalse,
		Reason:  v1alpha1.InstallInProgressReason,
		Message: message,
	})
	failedUpdated := setClusterInstallCondition(&ici.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hivev1.ClusterInstallFailed,
		Status:  corev1.ConditionFalse,
		Reason:  v1alpha1.InstallInProgressReason,
		Message: v1alpha1.InstallInProgressMessage,
	})
	if !completedUpdated && !stoppedUpdated && !failedUpdated {
		return nil
	}

	r.Log.Info("Setting cluster install conditions")
	return r.Status().Patch(ctx, ici, patch)
}

func (r *ImageClusterInstallMonitor) setClusterTimeoutConditions(ctx context.Context, ici *v1alpha1.ImageClusterInstall, timeout string) error {
	message := fmt.Sprintf("Cluster failed to install within the timeout (%s)", timeout)
	patch := client.MergeFrom(ici.DeepCopy())
	completedUpdated := setClusterInstallCondition(&ici.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hivev1.ClusterInstallCompleted,
		Status:  corev1.ConditionFalse,
		Reason:  v1alpha1.InstallTimedoutReason,
		Message: message,
	})
	stoppedUpdated := setClusterInstallCondition(&ici.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hivev1.ClusterInstallStopped,
		Status:  corev1.ConditionFalse,
		Reason:  v1alpha1.InstallTimedoutReason,
		Message: v1alpha1.InstallTimedoutMessage,
	})
	failedUpdated := setClusterInstallCondition(&ici.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hivev1.ClusterInstallFailed,
		Status:  corev1.ConditionTrue,
		Reason:  v1alpha1.InstallTimedoutReason,
		Message: message,
	})

	if !completedUpdated && !stoppedUpdated && !failedUpdated {
		return nil
	}

	r.Log.Info("Setting cluster timeout conditions")
	return r.Status().Patch(ctx, ici, patch)
}

func (r *ImageClusterInstallMonitor) setClusterInstalledConditions(ctx context.Context, ici *v1alpha1.ImageClusterInstall) error {
	patch := client.MergeFrom(ici.DeepCopy())
	completedUpdated := setClusterInstallCondition(&ici.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hivev1.ClusterInstallCompleted,
		Status:  corev1.ConditionTrue,
		Reason:  v1alpha1.InstallSucceededReason,
		Message: v1alpha1.InstallSucceededMessage,
	})
	stoppedUpdated := setClusterInstallCondition(&ici.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hivev1.ClusterInstallStopped,
		Status:  corev1.ConditionTrue,
		Reason:  v1alpha1.InstallSucceededReason,
		Message: v1alpha1.InstallSucceededMessage,
	})
	failedUpdated := setClusterInstallCondition(&ici.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hivev1.ClusterInstallFailed,
		Status:  corev1.ConditionFalse,
		Reason:  v1alpha1.InstallSucceededReason,
		Message: v1alpha1.InstallSucceededMessage,
	})

	if !completedUpdated && !stoppedUpdated && !failedUpdated {
		return nil
	}

	r.Log.Info("Setting cluster installed conditions")

	return r.Status().Patch(ctx, ici, patch)
}
