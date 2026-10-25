package auth

import "net/http"

func (s *Service) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.Roles(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": roles, "permissions": Permissions()})
}

func (s *Service) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        Role     `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	by, _ := UserFrom(r.Context())
	role, err := s.CreateRole(r.Context(), by, req.Name, req.Description, req.Permissions)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, role)
}

func (s *Service) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        *Role     `json:"name"`
		Description *string   `json:"description"`
		Permissions *[]string `json:"permissions"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	by, _ := UserFrom(r.Context())
	role, err := s.UpdateRole(r.Context(), by, Role(r.PathValue("name")), RoleChange{Name: req.Name, Description: req.Description, Permissions: req.Permissions})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, role)
}

func (s *Service) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !readJSON(w, r, &req) {
		return
	}
	by, _ := UserFrom(r.Context())
	if err := s.DeleteRole(r.Context(), by, Role(r.PathValue("name"))); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
