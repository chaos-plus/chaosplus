package authz

import "encoding/json"

// ResourceRef identifies an authorization scope without coupling IAM to the
// business object stored below that scope.
type ResourceRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// DataConstraint is the parameter-only result business repositories consume
// when filtering tenant-owned rows. Empty slices are intentional: callers can
// serialize and inspect the contract without null handling.
type DataConstraint struct {
	AllowAll      bool          `json:"allow_all"`
	OwnerIDs      []string      `json:"owner_ids"`
	ResourceIDs   []string      `json:"resource_ids"`
	DepartmentIDs []string      `json:"department_ids"`
	Ancestors     []ResourceRef `json:"ancestors"`
	DeniedIDs     []string      `json:"denied_ids"`
	Revision      int64         `json:"revision"`
}

// RelationshipStep is one persisted edge in a relationship authorization path.
type RelationshipStep struct {
	EntityID        string          `json:"entity_id,omitempty"`
	SubjectType     string          `json:"subject_type"`
	SubjectID       string          `json:"subject_id"`
	SubjectRelation string          `json:"subject_relation,omitempty"`
	Relation        string          `json:"relation"`
	ResourceType    string          `json:"resource_type"`
	ResourceID      string          `json:"resource_id"`
	Condition       json.RawMessage `json:"condition,omitempty"`
}

// DecisionMatch records one persisted grant or denial that participated in a
// decision. It contains policy metadata only, never credentials or row data.
type DecisionMatch struct {
	PermissionCode string             `json:"permission_code"`
	RoleID         string             `json:"role_id"`
	SourceType     string             `json:"source_type"`
	SourceID       string             `json:"source_id"`
	ScopeType      string             `json:"scope_type"`
	ScopeID        string             `json:"scope_id"`
	Effect         string             `json:"effect"`
	Inherited      bool               `json:"inherited"`
	Relation       string             `json:"relation,omitempty"`
	Path           []RelationshipStep `json:"path,omitempty"`
}

// Explanation is the inspectable form of the same entity decision used by
// request-time enforcement.
type Explanation struct {
	Allowed  bool            `json:"allowed"`
	Reason   string          `json:"reason"`
	Revision int64           `json:"revision"`
	Matches  []DecisionMatch `json:"matches"`
}
