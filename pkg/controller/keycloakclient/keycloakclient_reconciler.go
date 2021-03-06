package keycloakclient

import (
	"fmt"

	kc "github.com/keycloak/keycloak-operator/pkg/apis/keycloak/v1alpha1"
	"github.com/keycloak/keycloak-operator/pkg/common"
	"github.com/keycloak/keycloak-operator/pkg/model"
)

type Reconciler interface {
	Reconcile(cr *kc.KeycloakClient) error
}

type KeycloakClientReconciler struct { // nolint
	Keycloak kc.Keycloak
}

func NewKeycloakClientReconciler(keycloak kc.Keycloak) *KeycloakClientReconciler {
	return &KeycloakClientReconciler{
		Keycloak: keycloak,
	}
}

func (i *KeycloakClientReconciler) Reconcile(state *common.ClientState, cr *kc.KeycloakClient) common.DesiredClusterState {
	desired := common.DesiredClusterState{}

	desired.AddAction(i.pingKeycloak())
	if cr.DeletionTimestamp != nil {
		desired.AddAction(i.getDeletedClientState(state, cr))
		return desired
	}

	if state.Client == nil {
		desired.AddAction(i.getCreatedClientState(state, cr))
	} else {
		desired.AddAction(i.getUpdatedClientState(state, cr))
	}

	if state.ClientSecret == nil {
		desired.AddAction(i.getCreatedClientSecretState(state, cr))
	} else {
		desired.AddAction(i.getUpdatedClientSecretState(state, cr))
	}

	i.ReconcileRoles(state, cr, &desired)

	return desired
}

func (i *KeycloakClientReconciler) ReconcileRoles(state *common.ClientState, cr *kc.KeycloakClient, desired *common.DesiredClusterState) {
	// delete existing roles for which no desired role is found that (matches by ID OR has no ID but matches by name)
	// this implies that specifying a role with matching name but different ID will result in deletion (and re-creation)
	rolesDeleted, _ := roleDifferenceIntersection(state.Roles, cr.Spec.Roles)
	for _, role := range rolesDeleted {
		desired.AddAction(i.getDeletedClientRoleState(state, cr, role.DeepCopy()))
	}

	// update with desired roles that can be matched to existing roles and have an ID set, this includes all renames
	// note down all renames
	existingRoleByID := make(map[string]kc.RoleRepresentation)
	for _, role := range state.Roles {
		existingRoleByID[role.ID] = role
	}
	renamedRolesOldNames := make(map[string]bool)
	_, rolesMatching := roleDifferenceIntersection(cr.Spec.Roles, state.Roles)
	for _, role := range rolesMatching {
		if role.ID != "" {
			oldRole := existingRoleByID[role.ID]
			desired.AddAction(i.getUpdatedClientRoleState(state, cr, role.DeepCopy(), oldRole.DeepCopy()))
			if role.Name != oldRole.Name {
				renamedRolesOldNames[oldRole.Name] = true
			}
		}
	}

	// seemingly matching roles without an ID can either be regular updates
	// or re-creations after renames (not deletions)
	// note that duplicate role names are impossible thanks to +listType=map
	for _, role := range rolesMatching {
		if role.ID == "" {
			if _, contains := renamedRolesOldNames[role.Name]; contains {
				desired.AddAction(i.getCreatedClientRoleState(state, cr, role.DeepCopy()))
			} else {
				desired.AddAction(i.getUpdatedClientRoleState(state, cr, role.DeepCopy(), role.DeepCopy()))
			}
		}
	}

	// always create roles that don't match any existing ones
	rolesNew, _ := roleDifferenceIntersection(cr.Spec.Roles, state.Roles)
	for _, role := range rolesNew {
		desired.AddAction(i.getCreatedClientRoleState(state, cr, role.DeepCopy()))
	}
}

// returned roles are always from a
func roleDifferenceIntersection(a []kc.RoleRepresentation, b []kc.RoleRepresentation) (d []kc.RoleRepresentation, i []kc.RoleRepresentation) {
	for _, role := range a {
		if hasMatchingRole(b, role) {
			i = append(i, role)
		} else {
			d = append(d, role)
		}
	}
	return d, i
}

func hasMatchingRole(roles []kc.RoleRepresentation, otherRole kc.RoleRepresentation) bool {
	for _, role := range roles {
		if roleMatches(role, otherRole) {
			return true
		}
	}
	return false
}

func roleMatches(a kc.RoleRepresentation, b kc.RoleRepresentation) bool {
	if a.ID != "" && b.ID != "" {
		return a.ID == b.ID
	}
	return a.Name == b.Name
}

func (i *KeycloakClientReconciler) pingKeycloak() common.ClusterAction {
	return common.PingAction{
		Msg: "check if keycloak is available",
	}
}

func (i *KeycloakClientReconciler) getDeletedClientState(state *common.ClientState, cr *kc.KeycloakClient) common.ClusterAction {
	return common.DeleteClientAction{
		Ref:   cr,
		Realm: state.Realm.Spec.Realm.Realm,
		Msg:   fmt.Sprintf("removing client %v/%v", cr.Namespace, cr.Spec.Client.ClientID),
	}
}

func (i *KeycloakClientReconciler) getCreatedClientState(state *common.ClientState, cr *kc.KeycloakClient) common.ClusterAction {
	return common.CreateClientAction{
		Ref:   cr,
		Realm: state.Realm.Spec.Realm.Realm,
		Msg:   fmt.Sprintf("create client %v/%v", cr.Namespace, cr.Spec.Client.ClientID),
	}
}

func (i *KeycloakClientReconciler) getUpdatedClientSecretState(state *common.ClientState, cr *kc.KeycloakClient) common.ClusterAction {
	return common.GenericUpdateAction{
		Ref: model.ClientSecretReconciled(cr, state.ClientSecret),
		Msg: fmt.Sprintf("update client secret %v/%v", cr.Namespace, cr.Spec.Client.ClientID),
	}
}

func (i *KeycloakClientReconciler) getUpdatedClientState(state *common.ClientState, cr *kc.KeycloakClient) common.ClusterAction {
	return common.UpdateClientAction{
		Ref:   cr,
		Realm: state.Realm.Spec.Realm.Realm,
		Msg:   fmt.Sprintf("update client %v/%v", cr.Namespace, cr.Spec.Client.ClientID),
	}
}

func (i *KeycloakClientReconciler) getCreatedClientSecretState(state *common.ClientState, cr *kc.KeycloakClient) common.ClusterAction {
	return common.GenericCreateAction{
		Ref: model.ClientSecret(cr),
		Msg: fmt.Sprintf("create client secret %v/%v", cr.Namespace, cr.Spec.Client.ClientID),
	}
}

func (i *KeycloakClientReconciler) getCreatedClientRoleState(state *common.ClientState, cr *kc.KeycloakClient, role *kc.RoleRepresentation) common.ClusterAction {
	return common.CreateClientRoleAction{
		Role:  role,
		Ref:   cr,
		Realm: state.Realm.Spec.Realm.Realm,
		Msg:   fmt.Sprintf("create client role %v/%v/%v", cr.Namespace, cr.Spec.Client.ClientID, role.Name),
	}
}

func (i *KeycloakClientReconciler) getUpdatedClientRoleState(state *common.ClientState, cr *kc.KeycloakClient, role, oldRole *kc.RoleRepresentation) common.ClusterAction {
	return common.UpdateClientRoleAction{
		Role:    role,
		OldRole: oldRole,
		Ref:     cr,
		Realm:   state.Realm.Spec.Realm.Realm,
		Msg:     fmt.Sprintf("update client role %v/%v/%v", cr.Namespace, cr.Spec.Client.ClientID, oldRole.Name),
	}
}

func (i *KeycloakClientReconciler) getDeletedClientRoleState(state *common.ClientState, cr *kc.KeycloakClient, role *kc.RoleRepresentation) common.ClusterAction {
	return common.DeleteClientRoleAction{
		Role:  role,
		Ref:   cr,
		Realm: state.Realm.Spec.Realm.Realm,
		Msg:   fmt.Sprintf("delete client role %v/%v/%v", cr.Namespace, cr.Spec.Client.ClientID, role.Name),
	}
}
