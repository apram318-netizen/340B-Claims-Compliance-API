package main

import (
	"claims-system/internal/database"
	"claims-system/internal/feature"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// SCIM 2.0 endpoints (Bearer token). See docs/integrator-guide.md.

func (apiCfg *apiConfig) middlewareSCIM(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org := scimOrgID()
		if org == uuid.Nil || apiCfg.Features == nil || !apiCfg.Features.Enabled(r.Context(), org, feature.SCIM) {
			respondWithError(w, http.StatusNotFound, "not found")
			return
		}
		want := strings.TrimSpace(os.Getenv("SCIM_BEARER_TOKEN"))
		if want == "" {
			respondWithError(w, http.StatusNotFound, "not found")
			return
		}
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			respondWithError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		got := strings.TrimSpace(auth[7:])
		if got != want {
			respondWithError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func scimOrgID() uuid.UUID {
	s := strings.TrimSpace(os.Getenv("SCIM_ORG_ID"))
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func (apiCfg *apiConfig) handlerSCIMServiceProviderConfig(w http.ResponseWriter, r *http.Request) {
	respondWithJSON(w, http.StatusOK, map[string]any{
		"schemas":          []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"documentationUri": "https://example.com/scim",
		"patch":            map[string]bool{"supported": true},
		"filter":           map[string]bool{"supported": true, "maxResults": true},
		"changePassword":   map[string]bool{"supported": false},
		"sort":             map[string]bool{"supported": false},
		"etag":             map[string]bool{"supported": false},
		"authenticationSchemes": []map[string]any{
			{"type": "oauthbearertoken", "name": "OAuth Bearer Token", "primary": true},
		},
	})
}

func (apiCfg *apiConfig) handlerSCIMResourceTypes(w http.ResponseWriter, r *http.Request) {
	respondWithJSON(w, http.StatusOK, map[string]any{
		"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		"totalResults": 1,
		"Resources": []map[string]any{
			{
				"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"},
				"id":          "User",
				"name":        "User",
				"endpoint":    "/Users",
				"description": "User Account",
				"schema":      "urn:ietf:params:scim:schemas:core:2.0:User",
			},
		},
	})
}

func (apiCfg *apiConfig) handlerSCIMSchemas(w http.ResponseWriter, r *http.Request) {
	respondWithJSON(w, http.StatusOK, map[string]any{
		"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		"totalResults": 1,
		"Resources":    []map[string]any{scimUserSchema()},
	})
}

func (apiCfg *apiConfig) handlerSCIMSchemaByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id != "urn:ietf:params:scim:schemas:core:2.0:User" {
		respondWithSCIMError(w, http.StatusNotFound, "not found")
		return
	}
	respondWithJSON(w, http.StatusOK, scimUserSchema())
}

func scimUserSchema() map[string]any {
	return map[string]any{
		"id":     "urn:ietf:params:scim:schemas:core:2.0:User",
		"name":   "User",
		"schemas": []string{
			"urn:ietf:params:scim:schemas:core:2.0:Schema",
		},
		"attributes": []map[string]any{
			{"name": "userName", "type": "string", "multiValued": false, "required": true},
			{"name": "name", "type": "complex", "multiValued": false},
			{"name": "emails", "type": "complex", "multiValued": true},
			{"name": "active", "type": "boolean", "multiValued": false},
		},
	}
}

func (apiCfg *apiConfig) handlerSCIMListUsers(w http.ResponseWriter, r *http.Request) {
	orgID := scimOrgID()
	if orgID == uuid.Nil {
		respondWithSCIMError(w, http.StatusBadRequest, "SCIM_ORG_ID not configured")
		return
	}
	filter := strings.TrimSpace(r.URL.Query().Get("filter"))
	if filter != "" {
		if email, ok := parseSCIMAttributeFilter(filter, "userName"); ok {
			u, err := apiCfg.DB.GetUserByEmail(r.Context(), strings.ToLower(email))
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					respondWithJSON(w, http.StatusOK, scimListResponse([]any{}, 0, 1, 0))
					return
				}
				slog.Error("scim filter user", "error", err)
				respondWithSCIMError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if u.OrgID != orgID {
				respondWithJSON(w, http.StatusOK, scimListResponse([]any{}, 0, 1, 0))
				return
			}
			respondWithJSON(w, http.StatusOK, scimListResponse([]any{scimUserResource(u)}, 1, 1, 1))
			return
		}
		if idStr, ok := parseSCIMAttributeFilter(filter, "id"); ok {
			id, err := uuid.Parse(idStr)
			if err != nil {
				respondWithSCIMError(w, http.StatusBadRequest, "invalid filter")
				return
			}
			u, err := apiCfg.DB.GetUserByIDForOrg(r.Context(), database.GetUserByIDForOrgParams{
				ID:    id,
				OrgID: orgID,
			})
			if err != nil {
				respondWithJSON(w, http.StatusOK, scimListResponse([]any{}, 0, 1, 0))
				return
			}
			respondWithJSON(w, http.StatusOK, scimListResponse([]any{scimUserResource(u)}, 1, 1, 1))
			return
		}
		respondWithSCIMError(w, http.StatusBadRequest, "unsupported filter")
		return
	}

	start := 1
	if v := r.URL.Query().Get("startIndex"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			start = n
		}
	}
	count := 100
	if v := r.URL.Query().Get("count"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			count = n
		}
	}
	offset := start - 1
	total, err := apiCfg.DB.CountUsersByOrg(r.Context(), orgID)
	if err != nil {
		slog.Error("scim count users", "error", err)
		respondWithSCIMError(w, http.StatusInternalServerError, "internal error")
		return
	}
	rows, err := apiCfg.DB.ListUsersByOrgPaginated(r.Context(), database.ListUsersByOrgPaginatedParams{
		OrgID:  orgID,
		Limit:  int32(count),
		Offset: int32(offset),
	})
	if err != nil {
		slog.Error("scim list users", "error", err)
		respondWithSCIMError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]any, 0, len(rows))
	for _, u := range rows {
		out = append(out, scimUserResource(u))
	}
	respondWithJSON(w, http.StatusOK, scimListResponse(out, total, start, len(out)))
}

var scimAttrEqRE = regexp.MustCompile(`(?i)^\s*([a-zA-Z0-9_.]+)\s+eq\s+"([^"]*)"\s*$`)

func parseSCIMAttributeFilter(filter, attr string) (string, bool) {
	m := scimAttrEqRE.FindStringSubmatch(filter)
	if len(m) != 3 || strings.ToLower(m[1]) != strings.ToLower(attr) {
		return "", false
	}
	return m[2], true
}

func scimListResponse(resources []any, total int64, startIndex, itemsInPage int) map[string]any {
	return map[string]any{
		"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		"totalResults": total,
		"startIndex":   startIndex,
		"itemsPerPage": itemsInPage,
		"Resources":    resources,
	}
}

func scimUserResource(u database.User) map[string]any {
	parts := strings.Fields(u.Name)
	given := ""
	family := ""
	if len(parts) > 0 {
		given = parts[0]
	}
	if len(parts) > 1 {
		family = strings.Join(parts[1:], " ")
	}
	return map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		"id":          u.ID.String(),
		"userName":    u.Email,
		"active":      u.Active,
		"name":        map[string]any{"givenName": given, "familyName": family},
		"displayName": u.Name,
		"emails": []map[string]any{
			{"value": u.Email, "primary": true},
		},
		"meta": map[string]any{
			"resourceType": "User",
		},
	}
}

func (apiCfg *apiConfig) handlerSCIMCreateUser(w http.ResponseWriter, r *http.Request) {
	orgID := scimOrgID()
	if orgID == uuid.Nil {
		respondWithSCIMError(w, http.StatusBadRequest, "SCIM_ORG_ID not configured")
		return
	}
	var payload struct {
		UserName string `json:"userName"`
		Name     struct {
			GivenName  string `json:"givenName"`
			FamilyName string `json:"familyName"`
		} `json:"name"`
		Emails []struct {
			Value string `json:"value"`
		} `json:"emails"`
		Active *bool `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondWithValidationIssues(w, r, "invalid JSON", nil)
		return
	}
	email := strings.ToLower(strings.TrimSpace(payload.UserName))
	if email == "" && len(payload.Emails) > 0 {
		email = strings.ToLower(strings.TrimSpace(payload.Emails[0].Value))
	}
	if email == "" {
		respondWithSCIMError(w, http.StatusBadRequest, "userName or emails required")
		return
	}
	name := strings.TrimSpace(payload.Name.GivenName + " " + payload.Name.FamilyName)
	if name == "" {
		name = email
	}
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	hash, err := bcrypt.GenerateFromPassword(raw, bcrypt.DefaultCost)
	if err != nil {
		respondWithSCIMError(w, http.StatusInternalServerError, "internal error")
		return
	}
	active := true
	if payload.Active != nil {
		active = *payload.Active
	}
	u, err := apiCfg.DB.CreateUser(r.Context(), database.CreateUserParams{
		OrgID:        orgID,
		Email:        email,
		Name:         name,
		Role:         "member",
		PasswordHash: string(hash),
	})
	if err != nil {
		slog.Error("scim create user", "error", err)
		respondWithSCIMError(w, http.StatusConflict, "user may already exist")
		return
	}
	if !active {
		u, err = apiCfg.DB.UpdateUserActive(r.Context(), database.UpdateUserActiveParams{
			ID:     u.ID,
			Active: false,
		})
		if err != nil {
			slog.Error("scim deactivate new user", "error", err)
		}
	}
	respondWithJSON(w, http.StatusCreated, scimUserResource(u))
}

func (apiCfg *apiConfig) handlerSCIMGetUser(w http.ResponseWriter, r *http.Request) {
	orgID := scimOrgID()
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithSCIMError(w, http.StatusBadRequest, "invalid id")
		return
	}
	u, err := apiCfg.DB.GetUserByIDForOrg(r.Context(), database.GetUserByIDForOrgParams{ID: id, OrgID: orgID})
	if err != nil {
		respondWithSCIMError(w, http.StatusNotFound, "not found")
		return
	}
	respondWithJSON(w, http.StatusOK, scimUserResource(u))
}

func (apiCfg *apiConfig) handlerSCIMPatchUser(w http.ResponseWriter, r *http.Request) {
	orgID := scimOrgID()
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithSCIMError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var doc struct {
		Schemas    []string `json:"schemas"`
		Operations []struct {
			Op    string          `json:"op"`
			Path  string          `json:"path"`
			Value json.RawMessage `json:"value"`
		} `json:"Operations"`
	}
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		respondWithSCIMError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	u, err := apiCfg.DB.GetUserByIDForOrg(r.Context(), database.GetUserByIDForOrgParams{ID: id, OrgID: orgID})
	if err != nil {
		respondWithSCIMError(w, http.StatusNotFound, "not found")
		return
	}
	for _, op := range doc.Operations {
		switch strings.ToLower(op.Op) {
		case "replace":
			switch strings.TrimSpace(op.Path) {
			case "active":
				var v bool
				if err := json.Unmarshal(op.Value, &v); err != nil {
					respondWithSCIMError(w, http.StatusBadRequest, "invalid active value")
					return
				}
				u, err = apiCfg.DB.UpdateUserActive(r.Context(), database.UpdateUserActiveParams{ID: u.ID, Active: v})
				if err != nil {
					respondWithSCIMError(w, http.StatusInternalServerError, "internal error")
					return
				}
			case "name.givenName", "name.familyName":
				var s string
				_ = json.Unmarshal(op.Value, &s)
				parts := strings.Fields(u.Name)
				given := ""
				family := ""
				if len(parts) > 0 {
					given = parts[0]
				}
				if len(parts) > 1 {
					family = strings.Join(parts[1:], " ")
				}
				if strings.HasSuffix(op.Path, "givenName") && s != "" {
					given = s
				}
				if strings.HasSuffix(op.Path, "familyName") && s != "" {
					family = s
				}
				newName := strings.TrimSpace(strings.TrimSpace(given + " " + family))
				if newName == "" {
					newName = u.Email
				}
				u, err = apiCfg.DB.UpdateUserName(r.Context(), database.UpdateUserNameParams{ID: u.ID, Name: newName})
				if err != nil {
					respondWithSCIMError(w, http.StatusInternalServerError, "internal error")
					return
				}
			default:
				if op.Path == "" {
					var rep map[string]any
					if err := json.Unmarshal(op.Value, &rep); err == nil {
						if v, ok := rep["active"].(bool); ok {
							u, err = apiCfg.DB.UpdateUserActive(r.Context(), database.UpdateUserActiveParams{ID: u.ID, Active: v})
							if err != nil {
								respondWithSCIMError(w, http.StatusInternalServerError, "internal error")
								return
							}
						}
					}
				}
			}
		}
	}
	respondWithJSON(w, http.StatusOK, scimUserResource(u))
}

func (apiCfg *apiConfig) handlerSCIMDeleteUser(w http.ResponseWriter, r *http.Request) {
	orgID := scimOrgID()
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithSCIMError(w, http.StatusBadRequest, "invalid id")
		return
	}
	_, err = apiCfg.DB.GetUserByIDForOrg(r.Context(), database.GetUserByIDForOrgParams{ID: id, OrgID: orgID})
	if err != nil {
		respondWithSCIMError(w, http.StatusNotFound, "not found")
		return
	}
	_, err = apiCfg.DB.UpdateUserActive(r.Context(), database.UpdateUserActiveParams{ID: id, Active: false})
	if err != nil {
		respondWithSCIMError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func respondWithSCIMError(w http.ResponseWriter, status int, detail string) {
	respondWithJSON(w, status, map[string]any{
		"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
		"detail":  detail,
		"status":  fmt.Sprintf("%d", status),
	})
}
