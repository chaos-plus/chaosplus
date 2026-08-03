package provisioning

type supportedFeature struct {
	Supported bool `json:"supported"`
}

type bulkFeature struct {
	Supported      bool `json:"supported"`
	MaxOperations  int  `json:"maxOperations"`
	MaxPayloadSize int  `json:"maxPayloadSize"`
}

type filterFeature struct {
	Supported  bool `json:"supported"`
	MaxResults int  `json:"maxResults"`
}

type ServiceProviderConfig struct {
	Schemas        []string         `json:"schemas"`
	Patch          supportedFeature `json:"patch"`
	Bulk           bulkFeature      `json:"bulk"`
	Filter         filterFeature    `json:"filter"`
	ChangePassword supportedFeature `json:"changePassword"`
	Sort           supportedFeature `json:"sort"`
	ETag           supportedFeature `json:"etag"`
}

type SCIMAttribute struct {
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	MultiValued   bool            `json:"multiValued"`
	Required      bool            `json:"required"`
	Mutability    string          `json:"mutability"`
	Returned      string          `json:"returned"`
	Uniqueness    string          `json:"uniqueness"`
	SubAttributes []SCIMAttribute `json:"subAttributes,omitempty"`
}

type SCIMSchemaResource struct {
	Schemas     []string        `json:"schemas"`
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Attributes  []SCIMAttribute `json:"attributes"`
}

type ResourceTypeResource struct {
	Schemas          []string `json:"schemas"`
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Endpoint         string   `json:"endpoint"`
	Description      string   `json:"description"`
	Schema           string   `json:"schema"`
	SchemaExtensions []any    `json:"schemaExtensions"`
}

const (
	serviceProviderSchema = "urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"
	schemaSchema          = "urn:ietf:params:scim:schemas:core:2.0:Schema"
	resourceTypeSchema    = "urn:ietf:params:scim:schemas:core:2.0:ResourceType"
)

func serviceProviderConfig() ServiceProviderConfig {
	return ServiceProviderConfig{
		Schemas: []string{serviceProviderSchema}, Patch: supportedFeature{true},
		Bulk: bulkFeature{true, maxBulkOps, maxBodyBytes}, Filter: filterFeature{true, maxPageSize},
		ChangePassword: supportedFeature{false}, Sort: supportedFeature{false}, ETag: supportedFeature{true},
	}
}

func schemaResources() []SCIMSchemaResource {
	stringAttr := func(name string, required bool, uniqueness string) SCIMAttribute {
		return SCIMAttribute{Name: name, Type: "string", Required: required, Mutability: "readWrite", Returned: "default", Uniqueness: uniqueness}
	}
	user := SCIMSchemaResource{Schemas: []string{schemaSchema}, ID: UserSchema, Name: "User", Description: "Chaosplus provisioned user", Attributes: []SCIMAttribute{
		stringAttr("userName", true, "server"), stringAttr("externalId", false, "server"), stringAttr("displayName", false, "none"),
		{Name: "name", Type: "complex", Mutability: "readWrite", Returned: "default", Uniqueness: "none", SubAttributes: []SCIMAttribute{stringAttr("formatted", false, "none")}},
		{Name: "active", Type: "boolean", Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
		{Name: "emails", Type: "complex", MultiValued: true, Mutability: "readWrite", Returned: "default", Uniqueness: "none", SubAttributes: []SCIMAttribute{stringAttr("value", false, "none"), stringAttr("type", false, "none"), {Name: "primary", Type: "boolean", Mutability: "readWrite", Returned: "default", Uniqueness: "none"}}},
	}}
	group := SCIMSchemaResource{Schemas: []string{schemaSchema}, ID: GroupSchema, Name: "Group", Description: "Chaosplus provisioned static group", Attributes: []SCIMAttribute{
		stringAttr("displayName", true, "server"), stringAttr("externalId", false, "server"),
		{Name: "members", Type: "complex", MultiValued: true, Mutability: "readWrite", Returned: "default", Uniqueness: "none", SubAttributes: []SCIMAttribute{stringAttr("value", true, "none"), {Name: "$ref", Type: "reference", Mutability: "readOnly", Returned: "default", Uniqueness: "none"}}},
	}}
	return []SCIMSchemaResource{user, group}
}

func resourceTypes() []ResourceTypeResource {
	return []ResourceTypeResource{
		{Schemas: []string{resourceTypeSchema}, ID: ResourceUser, Name: ResourceUser, Endpoint: "/Users", Description: "Chaosplus provisioned users", Schema: UserSchema, SchemaExtensions: []any{}},
		{Schemas: []string{resourceTypeSchema}, ID: ResourceGroup, Name: ResourceGroup, Endpoint: "/Groups", Description: "Chaosplus provisioned groups", Schema: GroupSchema, SchemaExtensions: []any{}},
	}
}
