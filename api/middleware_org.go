package main

import (
	"net/http"

	"github.com/google/uuid"
)

// requireOrgAccess returns true if the authenticated user belongs to orgID (or is same-org admin for mutations).
func requireOrgAccess(w http.ResponseWriter, r *http.Request, orgID uuid.UUID) bool {
	au, err := getAuthUser(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	if au.OrgID != orgID {
		respondWithError(w, http.StatusForbidden, "access denied")
		return false
	}
	return true
}

func requireOrgAdmin(w http.ResponseWriter, r *http.Request, orgID uuid.UUID) bool {
	if !requireOrgAccess(w, r, orgID) {
		return false
	}
	au, _ := getAuthUser(r)
	if !isAdminRole(au.Role) {
		respondWithError(w, http.StatusForbidden, "org admin role required")
		return false
	}
	return true
}
